<script lang="ts">
    import { onMount, onDestroy } from "svelte";
    import {
        PlaySong,
        Stop,
        TogglePause,
        SeekForward,
        SeekBackward,
        SetVolume,
        ToggleLoop,
        SeekTo,
        GetState,
        CreatePlaylist,
        DeletePlaylist,
        AddSongToPlaylist,
        RemoveSongFromPlaylist,
        GetPlaylistState,
        PlayPlaylist,
        NextSong,
        PrevSong,
        SetPlaylistAlbumArt,
        CreatePlaylistFromExportify,
        ImportExportify,
        ReorderSong,
    } from "../wailsjs/go/main/App";
    import { EventsOn } from "../wailsjs/runtime/runtime";
    import type { main } from "../wailsjs/go/models";
    import Icon from "./components/Icon.svelte";

    // ── Navigation ──────────────────────────────────────────────────────────
    type Page = "home" | "library" | "playlist";
    let page: Page = "home";
    let selectedPLId: number | null = null;

    // ── Player state ─────────────────────────────────────────────────────────
    let searchQuery = "";
    let state: main.PlayerState = {
        playing: false,
        paused: false,
        loop: false,
        volume: 100,
        currentSong: "",
        position: 0,
        duration: 0,
    };
    let plState = {
        playlists: [],
        activePL: -1,
        activeSong: 0,
    } as unknown as main.PlaylistState;

    let loading = false;
    let error = "";
    let seeking = false;
    let displayPosition = 0;

    // ── UI state ─────────────────────────────────────────────────────────────
    let showNewPL = false;
    let newPLName = "";
    let addSongInput = "";
    let artInput: HTMLInputElement;
    let artUploadPLId: number | null = null;
    let csvInput: HTMLInputElement; // hidden file input for CSV
    let csvImportMode: "new" | "existing" = "new"; // which flow triggered the picker

    // ── Drag-to-reorder state ─────────────────────────────────────────────────
    let dragSrcIdx: number = -1;
    let dragOverIdx: number = -1;

    // ── Event listeners ──────────────────────────────────────────────────────
    let unlistenState: () => void;
    let unlistenPL: () => void;

    // ── Reactive ─────────────────────────────────────────────────────────────
    $: if (!seeking) displayPosition = state.position;
    $: isPlaying = state.playing && !state.paused;
    $: songTitle = state.currentSong.includes(" - ")
        ? state.currentSong.split(" - ").slice(1).join(" - ")
        : state.currentSong;
    $: artistName = state.currentSong.includes(" - ")
        ? state.currentSong.split(" - ")[0]
        : "";
    $: activePLObj =
        plState.activePL >= 0 ? plState.playlists[plState.activePL] : null;
    $: selectedPL =
        selectedPLId != null
            ? (plState.playlists.find((p) => p.id === selectedPLId) ?? null)
            : null;
    $: favoritesPlaylist = plState.playlists.find((p) => p.isFavorites) ?? null;
    $: activeArt = (activePLObj as any)?.albumArt ?? "";
    $: currentSongIsFav = state.currentSong ? isFav(state.currentSong) : false;

    // Precomputed for use in template (Svelte 3 forbids TypeScript "as" in template expressions)
    $: selectedPLIsFav = selectedPL ? selectedPL.isFavorites : false;
    $: selectedPLArt = selectedPL ? (selectedPL.albumArt ?? "") : "";
    $: selectedPLSongCount = selectedPL ? selectedPL.songs.length : 0;
    $: selectedPLIdSafe = selectedPL ? selectedPL.id : -1;
    $: selectedPLNameSafe = selectedPL ? selectedPL.name : "";
    $: activePLIdSafe = activePLObj ? activePLObj.id : -1;
    $: activePLNameSafe = activePLObj ? activePLObj.name : "";
    $: activePLCountSafe = activePLObj ? activePLObj.songs.length : 0;

    function isFav(name: string): boolean {
        return (
            (favoritesPlaylist as any)?.songs?.some(
                (s: any) => s.name === name,
            ) ?? false
        );
    }

    function formatTime(s: number): string {
        if (!s || isNaN(s)) return "0:00";
        return `${Math.floor(s / 60)}:${String(Math.floor(s % 60)).padStart(2, "0")}`;
    }

    function artStyle(art: string): string {
        if (!art) return "";
        return `background-image:url('${art}');background-size:cover;background-position:center;`;
    }

    // ── Player actions ───────────────────────────────────────────────────────
    async function handlePlay() {
        if (!searchQuery.trim()) return;
        loading = true;
        error = "";
        const err = await PlaySong(searchQuery.trim());
        if (err) {
            error = err;
            loading = false;
        }
    }

    function handleSeekInput(e: Event) {
        seeking = true;
        displayPosition = +(e.currentTarget as HTMLInputElement).value;
    }

    function handleSeekChange(e: Event) {
        seeking = false;
        SeekTo(+(e.currentTarget as HTMLInputElement).value);
    }

    function handleVolume(e: Event) {
        const v = +(e.currentTarget as HTMLInputElement).value;
        state = { ...state, volume: v };
        SetVolume(v);
    }

    // ── Playlist actions ─────────────────────────────────────────────────────
    async function createPL() {
        if (!newPLName.trim()) return;
        plState = await CreatePlaylist(newPLName.trim());
        newPLName = "";
        showNewPL = false;
    }

    async function deletePL(id: number) {
        plState = await DeletePlaylist(id);
        if (selectedPLId === id) {
            page = "library";
            selectedPLId = null;
        }
    }

    async function addSong(plID: number) {
        const name = addSongInput.trim();
        if (!name) return;
        plState = await AddSongToPlaylist(plID, name);
        addSongInput = "";
    }

    async function removeSong(plID: number, idx: number) {
        plState = await RemoveSongFromPlaylist(plID, idx);
    }

    async function playPL(plID: number, idx: number = 0) {
        loading = true;
        error = "";
        const err = await PlayPlaylist(plID, idx);
        if (err) {
            error = err;
            loading = false;
        }
    }

    async function toggleFav(songName: string) {
        if (!favoritesPlaylist) return;
        if (isFav(songName)) {
            const idx = (favoritesPlaylist as any).songs.findIndex(
                (s: any) => s.name === songName,
            );
            if (idx >= 0)
                plState = await RemoveSongFromPlaylist(
                    (favoritesPlaylist as any).id,
                    idx,
                );
        } else {
            plState = await AddSongToPlaylist(
                (favoritesPlaylist as any).id,
                songName,
            );
        }
    }

    // ── Exportify import ─────────────────────────────────────────────────────
    function triggerCSVImport(mode: "new" | "existing") {
        csvImportMode = mode;
        if (csvInput) csvInput.value = "";
        csvInput?.click();
    }

    async function handleCSVFile(e: Event) {
        const file = (e.target as HTMLInputElement).files?.[0];
        if (!file) return;
        const text = await file.text();
        if (csvImportMode === "new") {
            // Strip the .csv extension for the playlist name
            const name =
                file.name.replace(/\.csv$/i, "").trim() || "Imported Playlist";
            plState = await CreatePlaylistFromExportify(name, text);
            // Navigate to the newly created playlist
            const newPL = plState.playlists[plState.playlists.length - 1];
            if (newPL) goPlaylist(newPL.id);
        } else {
            plState = await ImportExportify(selectedPLIdSafe, text);
        }
    }

    // ── Song drag-to-reorder ─────────────────────────────────────────────────
    function onDragStart(e: DragEvent, i: number) {
        dragSrcIdx = i;
        if (e.dataTransfer) {
            e.dataTransfer.effectAllowed = "move";
            e.dataTransfer.setData("text/plain", String(i));
        }
    }

    function onDragOver(e: DragEvent, i: number) {
        e.preventDefault();
        if (e.dataTransfer) e.dataTransfer.dropEffect = "move";
        dragOverIdx = i;
    }

    function onDragLeave(e: DragEvent) {
        // Only clear if we're leaving the row entirely (not entering a child)
        const rel = e.relatedTarget as Node | null;
        if (!(e.currentTarget as HTMLElement).contains(rel)) {
            dragOverIdx = -1;
        }
    }

    async function onDrop(e: DragEvent, i: number) {
        e.preventDefault();
        dragOverIdx = -1;
        if (dragSrcIdx < 0 || dragSrcIdx === i) {
            dragSrcIdx = -1;
            return;
        }
        plState = await ReorderSong(selectedPLIdSafe, dragSrcIdx, i);
        dragSrcIdx = -1;
    }

    function onDragEnd() {
        dragSrcIdx = -1;
        dragOverIdx = -1;
    }

    // ── Album art upload ─────────────────────────────────────────────────────
    function triggerArtUpload(plID: number) {
        artUploadPLId = plID;
        if (artInput) artInput.value = "";
        artInput?.click();
    }

    function handleArtFile(e: Event) {
        const file = (e.target as HTMLInputElement).files?.[0];
        if (!file || artUploadPLId == null) return;
        const plID = artUploadPLId;
        const reader = new FileReader();
        reader.onload = async (ev) => {
            const dataURL = ev.target?.result as string;
            if (dataURL) plState = await SetPlaylistAlbumArt(plID, dataURL);
        };
        reader.readAsDataURL(file);
    }

    // ── Navigation ───────────────────────────────────────────────────────────
    function goPlaylist(id: number) {
        selectedPLId = id;
        addSongInput = "";
        page = "playlist";
    }

    function goFavorites() {
        if (favoritesPlaylist) goPlaylist((favoritesPlaylist as any).id);
        else page = "library";
    }

    onMount(() => {
        unlistenState = EventsOn("state", (s: main.PlayerState) => {
            state = s;
            if (s.playing) loading = false;
        });
        unlistenPL = EventsOn("playlist", (ps: main.PlaylistState) => {
            plState = ps;
        });
        GetState().then((s) => (state = s));
        GetPlaylistState().then((ps) => (plState = ps));
    });

    onDestroy(() => {
        if (unlistenState) unlistenState();
        if (unlistenPL) unlistenPL();
    });
</script>

<!-- Hidden file input for album art uploads -->
<input
    bind:this={artInput}
    type="file"
    accept="image/*"
    style="display:none"
    on:change={handleArtFile}
/>
<!-- Hidden file input for Exportify CSV imports -->
<input
    bind:this={csvInput}
    type="file"
    accept=".csv"
    style="display:none"
    on:change={handleCSVFile}
/>

<div class="shell">
    <!-- ═══════════════════════════════════════
         NAV RAIL
    ════════════════════════════════════════ -->
    <nav class="nav-rail">
        <div class="nav-logo">
            <Icon name="music" size={22} />
        </div>

        <button
            class="nav-item"
            class:nav-active={page === "home"}
            on:click={() => (page = "home")}
            title="Home"
        >
            <Icon name="home" size={20} />
            <span class="nav-label">Home</span>
        </button>

        <button
            class="nav-item"
            class:nav-active={page === "library" || page === "playlist"}
            on:click={() => (page = "library")}
            title="Library"
        >
            <Icon name="library" size={20} />
            <span class="nav-label">Library</span>
        </button>

        <button
            class="nav-item"
            class:nav-active={page === "playlist" && selectedPLIsFav}
            on:click={goFavorites}
            title="Favorites"
        >
            <Icon name="heart" size={20} />
            <span class="nav-label">Favorites</span>
        </button>
    </nav>

    <!-- ═══════════════════════════════════════
         MAIN CONTENT
    ════════════════════════════════════════ -->
    <div class="content-wrap">
        <!-- ─────────────── HOME PAGE ─────────────── -->
        {#if page === "home"}
            <div class="page page-home">
                <!-- Search -->
                <div class="search-row">
                    <div class="search-wrap">
                        <span class="search-icon">
                            <Icon name="search" size={15} />
                        </span>
                        <input
                            class="search-input"
                            type="text"
                            placeholder="Search for a song…"
                            bind:value={searchQuery}
                            on:keydown={(e) =>
                                e.key === "Enter" && handlePlay()}
                        />
                    </div>
                    <button
                        class="play-search-btn"
                        on:click={handlePlay}
                        disabled={loading || !searchQuery.trim()}
                    >
                        {#if loading}
                            <div class="btn-spinner"></div>
                        {:else}
                            <Icon name="play" size={13} />
                        {/if}
                        {loading ? "Loading…" : "PLAY"}
                    </button>
                </div>

                {#if error}
                    <div class="error-bar">
                        <Icon name="alertTriangle" size={14} />
                        <span>{error}</span>
                    </div>
                {/if}

                <!-- Player card -->
                <div class="player-card">
                    <div class="card-top">
                        <!-- Big spinning circle art -->
                        <div class="art-wrap-lg">
                            <div
                                class="art-circle art-lg"
                                class:art-spin={isPlaying}
                                style={artStyle(activeArt)}
                            ></div>
                            {#if !activeArt}
                                <div class="art-hole art-hole-lg"></div>
                            {/if}
                        </div>

                        <!-- Track info -->
                        <div class="track-info">
                            {#if loading}
                                <div class="loading-box">
                                    <div class="spinner-ring"></div>
                                    <span>Loading "{searchQuery}"…</span>
                                </div>
                            {:else if state.currentSong}
                                <p class="np-label">NOW PLAYING</p>
                                <h2 class="np-title">
                                    {songTitle || state.currentSong}
                                </h2>
                                {#if artistName}
                                    <p class="np-artist">{artistName}</p>
                                {/if}
                                <p class="np-dur">
                                    {formatTime(state.duration)}
                                </p>
                                <button
                                    class="fav-btn"
                                    class:fav-active={currentSongIsFav}
                                    on:click={() =>
                                        toggleFav(state.currentSong)}
                                    title={currentSongIsFav
                                        ? "Remove from Favorites"
                                        : "Add to Favorites"}
                                >
                                    <Icon name="heart" size={14} />
                                    {currentSongIsFav
                                        ? "Favorited"
                                        : "Favorite"}
                                </button>
                                {#if activePLObj}
                                    <p class="pl-ctx">
                                        <Icon name="listMusic" size={12} />
                                        {activePLNameSafe} · {plState.activeSong +
                                            1}/{activePLCountSafe}
                                    </p>
                                {/if}
                            {:else}
                                <p class="idle-msg">
                                    Search for a song to begin
                                </p>
                            {/if}
                        </div>
                    </div>

                    <!-- Stop -->
                    <div class="home-bottom">
                        <button
                            class="stop-btn"
                            on:click={() => {
                                loading = false;
                                error = "";
                                Stop();
                            }}
                            disabled={!state.playing && !loading}
                        >
                            <Icon name="squareIcon" size={13} />
                            STOP
                        </button>
                    </div>
                </div>
            </div>

            <!-- ─────────────── LIBRARY PAGE ─────────────── -->
        {:else if page === "library"}
            <div class="page page-library">
                <div class="library-header">
                    <h1 class="page-title">YOUR LIBRARY</h1>
                    <div class="library-header-actions">
                        <button
                            class="import-btn"
                            on:click={() => triggerCSVImport("new")}
                            title="Import playlist from Exportify CSV"
                        >
                            <Icon name="upload" size={14} />
                            Import from Exportify
                        </button>
                        <button
                            class="icon-btn"
                            on:click={() => (showNewPL = !showNewPL)}
                            title="New playlist"
                        >
                            <Icon name="plus" size={15} />
                        </button>
                    </div>
                </div>

                {#if showNewPL}
                    <div class="new-pl-row">
                        <input
                            class="pl-input"
                            placeholder="Playlist name…"
                            bind:value={newPLName}
                            on:keydown={(e) => e.key === "Enter" && createPL()}
                        />
                        <button
                            class="icon-btn accent"
                            on:click={createPL}
                            title="Create"
                        >
                            <Icon name="check" size={15} />
                        </button>
                        <button
                            class="icon-btn"
                            on:click={() => {
                                showNewPL = false;
                                newPLName = "";
                            }}
                            title="Cancel"
                        >
                            <Icon name="x" size={15} />
                        </button>
                    </div>
                {/if}

                <!-- Playlist grid -->
                <div class="pl-grid">
                    {#each plState.playlists as pl (pl.id)}
                        <div
                            class="pl-card"
                            class:pl-card-fav={pl.isFavorites}
                            class:pl-card-active={activePLIdSafe === pl.id}
                            on:click={() => goPlaylist(pl.id)}
                            role="button"
                            tabindex="0"
                            on:keydown={(e) =>
                                e.key === "Enter" && goPlaylist(pl.id)}
                        >
                            <div class="pl-card-art-wrap">
                                <div
                                    class="pl-card-art"
                                    style={artStyle(pl.albumArt ?? "")}
                                >
                                    {#if !pl.albumArt}
                                        {#if pl.isFavorites}
                                            <span
                                                class="card-art-icon fav-icon"
                                            >
                                                <Icon name="heart" size={34} />
                                            </span>
                                        {:else}
                                            <span class="card-art-icon">
                                                <Icon name="music2" size={34} />
                                            </span>
                                        {/if}
                                    {/if}
                                </div>
                                <button
                                    class="pl-card-play"
                                    on:click|stopPropagation={() =>
                                        playPL(pl.id)}
                                    title="Play playlist"
                                >
                                    <Icon name="play" size={20} />
                                </button>
                            </div>
                            <div class="pl-card-info">
                                <span class="pl-card-name">{pl.name}</span>
                                <span class="pl-card-count"
                                    >{pl.songs.length} songs</span
                                >
                            </div>
                        </div>
                    {/each}

                    {#if plState.playlists.length === 0}
                        <p class="empty-msg">
                            No playlists yet — hit
                            <Icon name="plus" size={12} /> to create one.
                        </p>
                    {/if}
                </div>
            </div>

            <!-- ─────────────── PLAYLIST DETAIL PAGE ─────────────── -->
        {:else if page === "playlist" && selectedPL}
            <div class="page page-playlist">
                <!-- Back -->
                <div class="pl-detail-header">
                    <button
                        class="back-btn"
                        on:click={() => (page = "library")}
                    >
                        <Icon name="arrowLeft" size={16} />
                        Library
                    </button>
                </div>

                <!-- Hero -->
                <div class="pl-hero">
                    <div class="pl-hero-art-wrap">
                        <div
                            class="pl-hero-art"
                            style={artStyle(selectedPLArt)}
                        >
                            {#if !selectedPLArt}
                                {#if selectedPLIsFav}
                                    <span class="hero-art-icon fav-icon">
                                        <Icon name="heart" size={52} />
                                    </span>
                                {:else}
                                    <span class="hero-art-icon">
                                        <Icon name="music2" size={52} />
                                    </span>
                                {/if}
                            {/if}
                        </div>
                        {#if !selectedPLIsFav}
                            <button
                                class="upload-art-btn"
                                on:click={() =>
                                    triggerArtUpload(selectedPLIdSafe)}
                                title="Upload album art"
                            >
                                <Icon name="upload" size={13} />
                                {selectedPLArt ? "Change Art" : "Upload Art"}
                            </button>
                        {/if}
                    </div>

                    <div class="pl-hero-info">
                        <p class="pl-hero-type">
                            {selectedPLIsFav ? "YOUR FAVORITES" : "PLAYLIST"}
                        </p>
                        <h2 class="pl-hero-name">{selectedPLNameSafe}</h2>
                        <p class="pl-hero-count">
                            {selectedPLSongCount} songs
                        </p>
                        <div class="pl-hero-actions">
                            <button
                                class="play-all-btn"
                                on:click={() => playPL(selectedPLIdSafe)}
                                disabled={selectedPLSongCount === 0}
                            >
                                <Icon name="play" size={15} />
                                Play All
                            </button>
                            {#if !selectedPLIsFav}
                                <button
                                    class="danger-btn"
                                    on:click={() => deletePL(selectedPLIdSafe)}
                                >
                                    <Icon name="trash2" size={15} />
                                    Delete
                                </button>
                                <button
                                    class="import-songs-btn"
                                    on:click={() =>
                                        triggerCSVImport("existing")}
                                    title="Import songs from Exportify CSV"
                                >
                                    <Icon name="upload" size={14} />
                                    Import Songs
                                </button>
                            {/if}
                        </div>
                    </div>
                </div>

                <!-- Song list -->
                <div class="song-table">
                    {#if selectedPLSongCount === 0}
                        <p class="empty-msg" style="padding:2rem 0">
                            {selectedPLIsFav
                                ? "No favorites yet — heart a song to add it here."
                                : "No songs yet. Add one below."}
                        </p>
                    {:else}
                        {#each selectedPL.songs as song, i (i)}
                            <div
                                class="song-row"
                                class:song-row-active={activePLIdSafe ===
                                    selectedPLIdSafe &&
                                    plState.activeSong === i}
                                class:drag-over={dragOverIdx === i}
                                class:dragging={dragSrcIdx === i}
                                draggable={true}
                                on:dragstart={(e) => onDragStart(e, i)}
                                on:dragover={(e) => onDragOver(e, i)}
                                on:dragleave={onDragLeave}
                                on:drop={(e) => onDrop(e, i)}
                                on:dragend={onDragEnd}
                            >
                                <span class="drag-handle">
                                    <Icon name="gripVertical" size={14} />
                                </span>
                                <span class="song-idx">{i + 1}</span>
                                <button
                                    class="song-play-btn"
                                    on:click={() => playPL(selectedPLIdSafe, i)}
                                    title="Play"
                                >
                                    {#if activePLIdSafe === selectedPLIdSafe && plState.activeSong === i && isPlaying}
                                        <Icon name="pause" size={13} />
                                    {:else}
                                        <Icon name="play" size={13} />
                                    {/if}
                                </button>
                                <span class="song-name">{song.name}</span>
                                <div class="song-actions">
                                    <button
                                        class="song-icon-btn"
                                        class:fav-active={isFav(song.name)}
                                        on:click={() => toggleFav(song.name)}
                                        title={isFav(song.name)
                                            ? "Remove from Favorites"
                                            : "Add to Favorites"}
                                    >
                                        <Icon name="heart" size={13} />
                                    </button>
                                    <button
                                        class="song-icon-btn danger"
                                        on:click={() =>
                                            removeSong(selectedPLIdSafe, i)}
                                        title="Remove"
                                    >
                                        <Icon name="x" size={13} />
                                    </button>
                                </div>
                            </div>
                        {/each}
                    {/if}
                </div>

                <!-- Add song (not for favorites) -->
                {#if !selectedPLIsFav}
                    <div class="add-song-section">
                        <div class="add-song-row">
                            <input
                                class="pl-input"
                                placeholder="Add song…"
                                bind:value={addSongInput}
                                on:keydown={(e) =>
                                    e.key === "Enter" &&
                                    addSong(selectedPLIdSafe)}
                            />
                            <button
                                class="icon-btn accent"
                                on:click={() => addSong(selectedPLIdSafe)}
                                title="Add"
                            >
                                <Icon name="plus" size={15} />
                            </button>
                            {#if state.currentSong}
                                <button
                                    class="add-current-btn"
                                    on:click={async () => {
                                        if (state.currentSong)
                                            plState = await AddSongToPlaylist(
                                                selectedPLIdSafe,
                                                state.currentSong,
                                            );
                                    }}
                                    title="Add currently playing song"
                                >
                                    <Icon name="music" size={13} />
                                    Add Current
                                </button>
                            {/if}
                        </div>
                    </div>
                {/if}
            </div>
        {/if}
    </div>
    <!-- end content-wrap -->

    <!-- ═══════════════════════════════════════
         BOTTOM PLAYER BAR
    ════════════════════════════════════════ -->
    <div class="bottom-player">
        <!-- Left: art + info -->
        <div class="bp-left">
            <div class="bp-art-wrap">
                <div
                    class="art-circle bp-art"
                    class:art-spin={isPlaying}
                    style={artStyle(activeArt)}
                ></div>
                {#if !activeArt}
                    <div class="art-hole bp-art-hole"></div>
                {/if}
            </div>
            <div class="bp-info">
                {#if state.currentSong}
                    <span class="bp-title"
                        >{songTitle || state.currentSong}</span
                    >
                    {#if artistName}
                        <span class="bp-artist">{artistName}</span>
                    {/if}
                {:else}
                    <span class="bp-idle">Nothing playing</span>
                {/if}
            </div>
        </div>

        <!-- Center: controls + seek bar -->
        <div class="bp-center">
            <div class="bp-controls">
                <button
                    class="ctrl"
                    on:click={() => PrevSong()}
                    disabled={!activePLObj}
                    title="Previous"
                >
                    <Icon name="skipBack" size={17} />
                </button>
                <button
                    class="ctrl"
                    on:click={() => SeekBackward()}
                    disabled={!state.playing}
                    title="Rewind 10s"
                >
                    <Icon name="rewind" size={15} />
                </button>
                <button
                    class="ctrl big-ctrl"
                    on:click={TogglePause}
                    disabled={!state.playing}
                    title={isPlaying ? "Pause" : "Resume"}
                >
                    <Icon name={isPlaying ? "pause" : "play"} size={19} />
                </button>
                <button
                    class="ctrl"
                    on:click={() => SeekForward()}
                    disabled={!state.playing}
                    title="Forward 10s"
                >
                    <Icon name="fastForward" size={15} />
                </button>
                <button
                    class="ctrl"
                    on:click={() => NextSong()}
                    disabled={!activePLObj}
                    title="Next"
                >
                    <Icon name="skipForward" size={17} />
                </button>
            </div>

            <div class="bp-seek">
                <span class="time-tag">{formatTime(displayPosition)}</span>
                <input
                    type="range"
                    class="seek-bar"
                    min="0"
                    max={state.duration || 100}
                    value={displayPosition}
                    on:mousedown={handleSeekInput}
                    on:input={handleSeekInput}
                    on:change={handleSeekChange}
                    disabled={!state.playing}
                />
                <span class="time-tag">{formatTime(state.duration)}</span>
            </div>
        </div>

        <!-- Right: loop + volume -->
        <div class="bp-right">
            <button
                class="ctrl loop-btn"
                class:tag-active={state.loop}
                on:click={ToggleLoop}
                title="Loop"
            >
                <Icon name="repeat" size={15} />
            </button>
            <div class="vol-row">
                <span class="vol-icon">
                    <Icon
                        name={state.volume === 0 ? "volumeX" : "volume2"}
                        size={15}
                    />
                </span>
                <input
                    type="range"
                    class="vol-bar"
                    min="0"
                    max="130"
                    value={state.volume}
                    on:input={handleVolume}
                />
                <span class="vol-val">{state.volume}%</span>
            </div>
        </div>
    </div>
</div>

<style>
    /* ── CSS Variables ──────────────────────────────────────────────────────── */
    :global(:root) {
        --palette-dark: #252422;
        --palette-mid: #403d39;
        --palette-accent: #ccc5b9;
        --palette-light: #fffcf2;
        --text-primary: #fffcf2;
        --text-secondary: #ccc5b9;
        --glass-bg: rgba(64, 61, 57, 0.65);
        --glass-border: rgba(255, 252, 242, 0.09);
        --danger: #f87171;
        --success: #4ade80;
        --fav-color: #f87171;
        --transition: 0.22s;
        --nav-w: 72px;
        --bar-h: 90px;
        --art-gradient: conic-gradient(
            #ccc5b9,
            #eb5e28,
            #fffcf2,
            #403d39,
            #ccc5b9
        );
    }

    /* ── Shell (grid layout) ────────────────────────────────────────────────── */
    .shell {
        display: grid;
        grid-template-columns: var(--nav-w) 1fr;
        grid-template-rows: 1fr var(--bar-h);
        height: 100vh;
        background: var(--palette-dark);
        overflow: hidden;
    }

    /* ── Nav Rail ───────────────────────────────────────────────────────────── */
    .nav-rail {
        grid-column: 1;
        grid-row: 1;
        background: #1b1917;
        border-right: 1px solid var(--glass-border);
        display: flex;
        flex-direction: column;
        align-items: center;
        padding: 0.6rem 0 1rem;
        gap: 0.1rem;
        overflow: hidden;
    }

    .nav-logo {
        color: var(--palette-accent);
        padding: 0.8rem 0.5rem 1rem;
        display: flex;
        align-items: center;
        justify-content: center;
    }

    .nav-item {
        display: flex;
        flex-direction: column;
        align-items: center;
        justify-content: center;
        gap: 0.22rem;
        width: 100%;
        padding: 0.55rem 0;
        background: transparent;
        border: none;
        color: rgba(204, 197, 185, 0.55);
        cursor: pointer;
        font-family: "Outfit", sans-serif;
        font-size: 0.56rem;
        font-weight: 600;
        letter-spacing: 0.06em;
        transition: color var(--transition);
        text-transform: uppercase;
    }
    .nav-item:hover {
        color: var(--text-primary);
    }
    .nav-item.nav-active {
        color: var(--palette-accent);
    }
    .nav-label {
        font-size: 0.55rem;
        letter-spacing: 0.07em;
    }

    /* ── Content Wrap ───────────────────────────────────────────────────────── */
    .content-wrap {
        grid-column: 2;
        grid-row: 1;
        overflow-y: auto;
        background: var(--palette-dark);
    }
    .content-wrap::-webkit-scrollbar {
        width: 5px;
    }
    .content-wrap::-webkit-scrollbar-track {
        background: transparent;
    }
    .content-wrap::-webkit-scrollbar-thumb {
        background: rgba(204, 197, 185, 0.18);
        border-radius: 3px;
    }

    /* ── Pages ──────────────────────────────────────────────────────────────── */
    .page {
        padding: 2rem 2.25rem;
        min-height: 100%;
    }

    /* ── HOME PAGE ──────────────────────────────────────────────────────────── */
    .page-home {
        display: flex;
        flex-direction: column;
        align-items: center;
        gap: 1.25rem;
    }

    .search-row {
        display: flex;
        gap: 0.5rem;
        width: 100%;
        max-width: 560px;
    }

    .search-wrap {
        flex: 1;
        display: flex;
        align-items: center;
        background: var(--palette-mid);
        border: 1px solid var(--glass-border);
        border-radius: 4px;
        padding: 0 0.8rem;
        transition: border-color var(--transition);
    }
    .search-wrap:focus-within {
        border-color: var(--palette-accent);
    }
    .search-icon {
        color: var(--text-secondary);
        display: flex;
        align-items: center;
        margin-right: 0.45rem;
        flex-shrink: 0;
    }
    .search-input {
        flex: 1;
        background: transparent;
        border: none;
        color: var(--text-primary);
        padding: 0.65rem 0;
        font-family: "Outfit", sans-serif;
        font-size: 0.95rem;
        outline: none;
    }
    .search-input::placeholder {
        color: var(--text-secondary);
    }

    .play-search-btn {
        display: flex;
        align-items: center;
        gap: 0.4rem;
        background: var(--palette-mid);
        border: 1px solid var(--palette-accent);
        color: var(--palette-accent);
        padding: 0.65rem 1.1rem;
        border-radius: 4px;
        font-family: "Outfit", sans-serif;
        font-weight: 700;
        font-size: 0.82rem;
        letter-spacing: 0.09em;
        cursor: pointer;
        transition:
            background var(--transition),
            color var(--transition);
    }
    .play-search-btn:hover:not(:disabled) {
        background: var(--palette-accent);
        color: var(--palette-dark);
    }
    .play-search-btn:disabled {
        opacity: 0.4;
        cursor: not-allowed;
    }

    .btn-spinner {
        width: 11px;
        height: 11px;
        border: 2px solid rgba(204, 197, 185, 0.3);
        border-top-color: currentColor;
        border-radius: 50%;
        animation: spin 0.8s linear infinite;
        flex-shrink: 0;
    }

    .error-bar {
        display: flex;
        align-items: center;
        gap: 0.4rem;
        color: var(--danger);
        font-size: 0.82rem;
        width: 100%;
        max-width: 560px;
    }

    .player-card {
        background: var(--glass-bg);
        border: 1px solid var(--glass-border);
        border-radius: 8px;
        padding: 1.75rem;
        width: 100%;
        max-width: 560px;
    }

    .card-top {
        display: flex;
        gap: 1.75rem;
        align-items: center;
    }

    /* ── Art Circles (shared) ───────────────────────────────────────────────── */
    .art-circle {
        border-radius: 50%;
        background-image: var(--art-gradient);
        background-size: cover;
        background-position: center;
    }

    .art-wrap-lg {
        position: relative;
        width: 160px;
        height: 160px;
        flex-shrink: 0;
    }
    .art-lg {
        position: absolute;
        inset: 0;
    }

    .art-hole {
        position: absolute;
        top: 50%;
        left: 50%;
        transform: translate(-50%, -50%);
        background: var(--palette-dark);
        border-radius: 50%;
        border: 2px solid var(--glass-border);
        pointer-events: none;
    }
    .art-hole-lg {
        width: 46px;
        height: 46px;
    }

    .art-spin {
        animation: spin 10s linear infinite;
    }

    /* ── Track Info ─────────────────────────────────────────────────────────── */
    .track-info {
        flex: 1;
        display: flex;
        flex-direction: column;
        gap: 0.32rem;
        min-width: 0;
    }
    .np-label {
        font-size: 0.6rem;
        font-weight: 700;
        letter-spacing: 0.18em;
        color: var(--palette-accent);
        text-transform: uppercase;
    }
    .np-title {
        font-size: 1.2rem;
        font-weight: 700;
        color: var(--text-primary);
        overflow: hidden;
        display: -webkit-box;
        -webkit-line-clamp: 2;
        line-clamp: 2;
        -webkit-box-orient: vertical;
    }
    .np-artist {
        font-size: 0.88rem;
        color: var(--text-secondary);
    }
    .np-dur {
        font-size: 0.75rem;
        color: var(--text-secondary);
        font-variant-numeric: tabular-nums;
    }
    .idle-msg {
        color: var(--text-secondary);
        font-style: italic;
        font-size: 0.9rem;
    }

    .loading-box {
        display: flex;
        flex-direction: column;
        align-items: center;
        gap: 0.8rem;
    }
    .loading-box span {
        color: var(--text-secondary);
        font-size: 0.85rem;
        text-align: center;
    }
    .spinner-ring {
        width: 30px;
        height: 30px;
        border: 3px solid var(--glass-border);
        border-top-color: var(--palette-accent);
        border-radius: 50%;
        animation: spin 0.85s linear infinite;
    }

    .fav-btn {
        display: flex;
        align-items: center;
        gap: 0.3rem;
        background: transparent;
        border: 1px solid var(--glass-border);
        color: var(--text-secondary);
        padding: 0.28rem 0.65rem;
        border-radius: 20px;
        font-family: "Outfit", sans-serif;
        font-size: 0.73rem;
        font-weight: 600;
        cursor: pointer;
        width: fit-content;
        margin-top: 0.15rem;
        transition:
            color var(--transition),
            border-color var(--transition),
            background var(--transition);
    }
    .fav-btn:hover {
        color: var(--fav-color);
        border-color: var(--fav-color);
    }
    .fav-btn.fav-active {
        color: var(--fav-color);
        border-color: var(--fav-color);
        background: rgba(248, 113, 113, 0.09);
    }

    .pl-ctx {
        display: flex;
        align-items: center;
        gap: 0.3rem;
        font-size: 0.7rem;
        color: var(--text-secondary);
        margin-top: 0.1rem;
    }

    .home-bottom {
        display: flex;
        justify-content: center;
        margin-top: 1.4rem;
    }

    .stop-btn {
        display: flex;
        align-items: center;
        gap: 0.4rem;
        background: transparent;
        border: 1px solid var(--glass-border);
        color: var(--text-secondary);
        padding: 0.35rem 0.85rem;
        border-radius: 4px;
        font-family: "Outfit", sans-serif;
        font-size: 0.76rem;
        font-weight: 700;
        letter-spacing: 0.07em;
        cursor: pointer;
        transition:
            color var(--transition),
            border-color var(--transition);
    }
    .stop-btn:hover:not(:disabled) {
        color: var(--danger);
        border-color: var(--danger);
    }
    .stop-btn:disabled {
        opacity: 0.35;
        cursor: not-allowed;
    }

    /* ── LIBRARY PAGE ───────────────────────────────────────────────────────── */
    .page-library {
        max-width: 1100px;
    }

    .library-header {
        display: flex;
        align-items: center;
        justify-content: space-between;
        margin-bottom: 1.75rem;
    }
    .page-title {
        font-size: 0.68rem;
        font-weight: 700;
        letter-spacing: 0.2em;
        color: var(--text-secondary);
        text-transform: uppercase;
    }

    .icon-btn {
        background: transparent;
        border: 1px solid var(--glass-border);
        color: var(--text-secondary);
        width: 30px;
        height: 30px;
        border-radius: 4px;
        cursor: pointer;
        display: flex;
        align-items: center;
        justify-content: center;
        flex-shrink: 0;
        transition:
            color var(--transition),
            border-color var(--transition);
    }
    .icon-btn:hover {
        color: var(--text-primary);
        border-color: var(--palette-accent);
    }
    .icon-btn.accent:hover {
        color: var(--palette-accent);
        border-color: var(--palette-accent);
    }

    .library-header-actions {
        display: flex;
        align-items: center;
        gap: 0.5rem;
    }

    .import-btn {
        display: flex;
        align-items: center;
        gap: 0.35rem;
        background: transparent;
        border: 1px solid var(--glass-border);
        color: var(--text-secondary);
        padding: 0.35rem 0.75rem;
        border-radius: 4px;
        font-family: "Outfit", sans-serif;
        font-size: 0.75rem;
        font-weight: 600;
        cursor: pointer;
        white-space: nowrap;
        transition:
            color var(--transition),
            border-color var(--transition);
    }
    .import-btn:hover {
        color: var(--palette-accent);
        border-color: var(--palette-accent);
    }

    .import-songs-btn {
        display: flex;
        align-items: center;
        gap: 0.4rem;
        background: transparent;
        border: 1px solid var(--glass-border);
        color: var(--text-secondary);
        padding: 0.65rem 1.2rem;
        border-radius: 30px;
        font-family: "Outfit", sans-serif;
        font-size: 0.82rem;
        font-weight: 600;
        cursor: pointer;
        transition:
            color var(--transition),
            border-color var(--transition);
    }
    .import-songs-btn:hover {
        color: var(--palette-accent);
        border-color: var(--palette-accent);
    }

    .new-pl-row {
        display: flex;
        gap: 0.5rem;
        margin-bottom: 1.75rem;
        max-width: 380px;
    }

    .pl-input {
        flex: 1;
        background: var(--palette-mid);
        border: 1px solid var(--glass-border);
        color: var(--text-primary);
        padding: 0.42rem 0.65rem;
        font-size: 0.85rem;
        border-radius: 4px;
        outline: none;
        font-family: "Outfit", sans-serif;
        min-width: 0;
        transition: border-color var(--transition);
    }
    .pl-input:focus {
        border-color: var(--palette-accent);
    }
    .pl-input::placeholder {
        color: var(--text-secondary);
    }

    /* Playlist grid */
    .pl-grid {
        display: grid;
        grid-template-columns: repeat(auto-fill, minmax(155px, 1fr));
        gap: 1.25rem;
    }

    .pl-card {
        cursor: pointer;
        border-radius: 8px;
        padding: 0.9rem;
        background: var(--glass-bg);
        border: 1px solid var(--glass-border);
        transition:
            background var(--transition),
            border-color var(--transition),
            transform var(--transition);
        user-select: none;
        outline: none;
    }
    .pl-card:hover {
        background: rgba(64, 61, 57, 0.95);
        border-color: rgba(204, 197, 185, 0.3);
        transform: translateY(-2px);
    }
    .pl-card:focus-visible {
        outline: 2px solid var(--palette-accent);
    }
    .pl-card.pl-card-active {
        border-color: rgba(204, 197, 185, 0.45);
    }
    .pl-card.pl-card-fav {
        border-color: rgba(248, 113, 113, 0.25);
    }
    .pl-card.pl-card-fav:hover {
        border-color: rgba(248, 113, 113, 0.55);
    }

    .pl-card-art-wrap {
        position: relative;
        width: 100%;
        padding-bottom: 100%;
        margin-bottom: 0.8rem;
        border-radius: 50%;
        overflow: hidden;
    }

    .pl-card-art {
        position: absolute;
        inset: 0;
        border-radius: 50%;
        background-image: var(--art-gradient);
        background-size: cover;
        background-position: center;
        display: flex;
        align-items: center;
        justify-content: center;
    }

    .card-art-icon {
        color: rgba(255, 252, 242, 0.35);
    }
    .fav-icon {
        color: rgba(248, 113, 113, 0.55);
    }

    .pl-card-play {
        position: absolute;
        bottom: 8%;
        right: 8%;
        width: 38px;
        height: 38px;
        border-radius: 50%;
        background: var(--palette-accent);
        color: var(--palette-dark);
        border: none;
        cursor: pointer;
        display: flex;
        align-items: center;
        justify-content: center;
        opacity: 0;
        transform: translateY(6px);
        transition:
            opacity 0.18s,
            transform 0.18s;
        box-shadow: 0 4px 14px rgba(0, 0, 0, 0.45);
    }
    .pl-card:hover .pl-card-play {
        opacity: 1;
        transform: translateY(0);
    }

    .pl-card-info {
        display: flex;
        flex-direction: column;
        gap: 0.18rem;
    }
    .pl-card-name {
        font-size: 0.86rem;
        font-weight: 600;
        color: var(--text-primary);
        overflow: hidden;
        text-overflow: ellipsis;
        white-space: nowrap;
    }
    .pl-card-count {
        font-size: 0.72rem;
        color: var(--text-secondary);
    }

    .empty-msg {
        display: flex;
        align-items: center;
        gap: 0.3rem;
        color: var(--text-secondary);
        font-style: italic;
        font-size: 0.85rem;
        grid-column: 1 / -1;
    }

    /* ── PLAYLIST DETAIL PAGE ───────────────────────────────────────────────── */
    .page-playlist {
        max-width: 820px;
        margin: 0 auto;
    }

    .pl-detail-header {
        margin-bottom: 1.75rem;
    }

    .back-btn {
        display: flex;
        align-items: center;
        gap: 0.4rem;
        background: transparent;
        border: none;
        color: var(--text-secondary);
        font-family: "Outfit", sans-serif;
        font-size: 0.85rem;
        font-weight: 600;
        cursor: pointer;
        padding: 0.2rem 0;
        transition: color var(--transition);
    }
    .back-btn:hover {
        color: var(--text-primary);
    }

    .pl-hero {
        display: flex;
        gap: 2rem;
        align-items: flex-end;
        margin-bottom: 2.25rem;
    }

    .pl-hero-art-wrap {
        display: flex;
        flex-direction: column;
        align-items: center;
        gap: 0.8rem;
        flex-shrink: 0;
    }

    .pl-hero-art {
        width: 180px;
        height: 180px;
        border-radius: 50%;
        background-image: var(--art-gradient);
        background-size: cover;
        background-position: center;
        display: flex;
        align-items: center;
        justify-content: center;
        overflow: hidden;
        box-shadow: 0 10px 40px rgba(0, 0, 0, 0.55);
    }
    .hero-art-icon {
        color: rgba(255, 252, 242, 0.35);
    }

    .upload-art-btn {
        display: flex;
        align-items: center;
        gap: 0.35rem;
        background: transparent;
        border: 1px solid var(--glass-border);
        color: var(--text-secondary);
        padding: 0.32rem 0.8rem;
        border-radius: 20px;
        font-family: "Outfit", sans-serif;
        font-size: 0.72rem;
        font-weight: 600;
        cursor: pointer;
        white-space: nowrap;
        transition:
            color var(--transition),
            border-color var(--transition);
    }
    .upload-art-btn:hover {
        color: var(--text-primary);
        border-color: var(--palette-accent);
    }

    .pl-hero-info {
        display: flex;
        flex-direction: column;
        gap: 0.45rem;
        min-width: 0;
    }
    .pl-hero-type {
        font-size: 0.6rem;
        font-weight: 700;
        letter-spacing: 0.18em;
        color: var(--text-secondary);
        text-transform: uppercase;
    }
    .pl-hero-name {
        font-size: 2.1rem;
        font-weight: 800;
        color: var(--text-primary);
        overflow: hidden;
        text-overflow: ellipsis;
        white-space: nowrap;
        line-height: 1.1;
    }
    .pl-hero-count {
        font-size: 0.85rem;
        color: var(--text-secondary);
    }
    .pl-hero-actions {
        display: flex;
        gap: 0.75rem;
        align-items: center;
        margin-top: 0.4rem;
    }

    .play-all-btn {
        display: flex;
        align-items: center;
        gap: 0.5rem;
        background: var(--palette-accent);
        border: none;
        color: var(--palette-dark);
        padding: 0.65rem 1.6rem;
        border-radius: 30px;
        font-family: "Outfit", sans-serif;
        font-weight: 700;
        font-size: 0.85rem;
        cursor: pointer;
        transition:
            transform 0.15s,
            background var(--transition);
    }
    .play-all-btn:hover:not(:disabled) {
        transform: scale(1.04);
        background: var(--palette-light);
    }
    .play-all-btn:disabled {
        opacity: 0.4;
        cursor: not-allowed;
    }

    .danger-btn {
        display: flex;
        align-items: center;
        gap: 0.4rem;
        background: transparent;
        border: 1px solid var(--glass-border);
        color: var(--text-secondary);
        padding: 0.65rem 1.2rem;
        border-radius: 30px;
        font-family: "Outfit", sans-serif;
        font-size: 0.82rem;
        font-weight: 600;
        cursor: pointer;
        transition:
            color var(--transition),
            border-color var(--transition);
    }
    .danger-btn:hover {
        color: var(--danger);
        border-color: var(--danger);
    }

    /* Song table */
    .song-table {
        border-top: 1px solid var(--glass-border);
        margin-bottom: 1.5rem;
    }

    .song-row {
        display: flex;
        align-items: center;
        gap: 0.75rem;
        padding: 0.55rem 0.25rem;
        border-bottom: 1px solid var(--glass-border);
        border-radius: 3px;
        transition: background var(--transition);
    }
    .song-row:hover {
        background: rgba(255, 252, 242, 0.03);
    }
    .song-row.song-row-active {
        background: rgba(204, 197, 185, 0.07);
    }

    .drag-handle {
        color: var(--text-secondary);
        opacity: 0.3;
        cursor: grab;
        display: flex;
        align-items: center;
        flex-shrink: 0;
        padding: 0 0.1rem;
        transition: opacity var(--transition);
    }
    .song-row:hover .drag-handle {
        opacity: 0.7;
    }
    .song-row.dragging {
        opacity: 0.4;
    }
    .song-row.drag-over {
        border-top: 2px solid var(--palette-accent);
    }

    .song-idx {
        width: 22px;
        text-align: center;
        font-size: 0.7rem;
        color: var(--text-secondary);
        font-variant-numeric: tabular-nums;
        flex-shrink: 0;
    }

    .song-play-btn {
        background: transparent;
        border: none;
        color: var(--text-secondary);
        cursor: pointer;
        display: flex;
        align-items: center;
        padding: 0.22rem;
        border-radius: 3px;
        transition: color var(--transition);
        flex-shrink: 0;
    }
    .song-play-btn:hover {
        color: var(--text-primary);
    }
    .song-row.song-row-active .song-play-btn {
        color: var(--palette-accent);
    }

    .song-name {
        flex: 1;
        font-size: 0.87rem;
        color: var(--text-primary);
        overflow: hidden;
        text-overflow: ellipsis;
        white-space: nowrap;
        min-width: 0;
    }
    .song-row.song-row-active .song-name {
        color: var(--palette-accent);
    }

    .song-actions {
        display: flex;
        gap: 0.15rem;
        flex-shrink: 0;
    }

    .song-icon-btn {
        background: transparent;
        border: none;
        color: var(--text-secondary);
        cursor: pointer;
        display: flex;
        align-items: center;
        padding: 0.28rem;
        border-radius: 3px;
        transition: color var(--transition);
    }
    .song-icon-btn:hover {
        color: var(--text-primary);
    }
    .song-icon-btn.fav-active {
        color: var(--fav-color);
    }
    .song-icon-btn.danger:hover {
        color: var(--danger);
    }

    .add-song-section {
        padding: 0.75rem 0 1.5rem;
    }
    .add-song-row {
        display: flex;
        gap: 0.5rem;
        align-items: center;
        max-width: 520px;
    }
    .add-current-btn {
        display: flex;
        align-items: center;
        gap: 0.35rem;
        background: transparent;
        border: 1px solid var(--glass-border);
        color: var(--text-secondary);
        padding: 0.42rem 0.8rem;
        border-radius: 4px;
        font-family: "Outfit", sans-serif;
        font-size: 0.76rem;
        font-weight: 600;
        cursor: pointer;
        white-space: nowrap;
        transition:
            color var(--transition),
            border-color var(--transition);
    }
    .add-current-btn:hover {
        color: var(--palette-accent);
        border-color: var(--palette-accent);
    }

    /* ── BOTTOM PLAYER BAR ──────────────────────────────────────────────────── */
    .bottom-player {
        grid-column: 1 / -1;
        grid-row: 2;
        background: #1b1917;
        border-top: 1px solid var(--glass-border);
        display: flex;
        align-items: center;
        padding: 0 1.5rem;
        gap: 1rem;
    }

    /* Left: art + song info */
    .bp-left {
        display: flex;
        align-items: center;
        gap: 0.75rem;
        width: 27%;
        min-width: 0;
    }

    .bp-art-wrap {
        position: relative;
        width: 46px;
        height: 46px;
        flex-shrink: 0;
    }
    .bp-art {
        width: 46px;
        height: 46px;
    }
    .bp-art-hole {
        width: 14px;
        height: 14px;
    }

    .bp-info {
        display: flex;
        flex-direction: column;
        gap: 0.12rem;
        min-width: 0;
    }
    .bp-title {
        font-size: 0.84rem;
        font-weight: 600;
        color: var(--text-primary);
        overflow: hidden;
        text-overflow: ellipsis;
        white-space: nowrap;
    }
    .bp-artist {
        font-size: 0.7rem;
        color: var(--text-secondary);
        overflow: hidden;
        text-overflow: ellipsis;
        white-space: nowrap;
    }
    .bp-idle {
        font-size: 0.8rem;
        color: var(--text-secondary);
        font-style: italic;
    }

    /* Center: controls + seek */
    .bp-center {
        flex: 1;
        display: flex;
        flex-direction: column;
        align-items: center;
        gap: 0.35rem;
    }

    .bp-controls {
        display: flex;
        align-items: center;
        gap: 0.35rem;
    }

    .ctrl {
        background: transparent;
        border: none;
        color: var(--text-secondary);
        cursor: pointer;
        display: flex;
        align-items: center;
        justify-content: center;
        width: 34px;
        height: 34px;
        border-radius: 50%;
        transition:
            color var(--transition),
            background var(--transition);
    }
    .ctrl:hover:not(:disabled) {
        color: var(--text-primary);
        background: rgba(255, 252, 242, 0.07);
    }
    .ctrl:disabled {
        opacity: 0.28;
        cursor: not-allowed;
    }

    .big-ctrl {
        width: 42px;
        height: 42px;
        background: var(--palette-accent);
        color: var(--palette-dark);
    }
    .big-ctrl:hover:not(:disabled) {
        background: var(--palette-light);
        color: var(--palette-dark);
    }
    .big-ctrl:disabled {
        opacity: 0.38;
    }

    .loop-btn {
        border-radius: 4px !important;
        width: 30px !important;
        height: 30px !important;
    }
    .loop-btn.tag-active {
        color: var(--success);
    }

    .bp-seek {
        display: flex;
        align-items: center;
        gap: 0.5rem;
        width: 100%;
        max-width: 500px;
    }
    .time-tag {
        color: var(--text-secondary);
        font-size: 0.67rem;
        font-variant-numeric: tabular-nums;
        min-width: 2.5rem;
        text-align: center;
        flex-shrink: 0;
    }
    .seek-bar {
        flex: 1;
    }

    /* Right: loop + volume */
    .bp-right {
        display: flex;
        align-items: center;
        gap: 0.6rem;
        width: 27%;
        justify-content: flex-end;
    }

    .vol-row {
        display: flex;
        align-items: center;
        gap: 0.4rem;
    }
    .vol-icon {
        color: var(--text-secondary);
        display: flex;
        align-items: center;
        flex-shrink: 0;
    }
    .vol-bar {
        width: 88px;
    }
    .vol-val {
        color: var(--text-secondary);
        font-size: 0.7rem;
        min-width: 2.4rem;
        font-variant-numeric: tabular-nums;
    }

    /* ── Keyframes ──────────────────────────────────────────────────────────── */
    @keyframes spin {
        to {
            transform: rotate(360deg);
        }
    }

    /* ── Responsive ─────────────────────────────────────────────────────────── */
    @media (max-width: 640px) {
        .card-top {
            flex-direction: column;
            align-items: center;
        }
        .art-wrap-lg {
            width: 130px;
            height: 130px;
        }
        .pl-hero {
            flex-direction: column;
            align-items: center;
            text-align: center;
        }
        .pl-hero-name {
            font-size: 1.5rem;
        }
        .bp-right {
            display: none;
        }
        .bp-left {
            width: auto;
            flex-shrink: 1;
        }
    }
</style>
