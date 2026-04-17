package main

import (
	"bufio"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	winio "github.com/Microsoft/go-winio"
	"github.com/OwlWorksInnovations/go-packages/configpath"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

const playlistsFile = ".moody/playlists.json"

// ──────────────────────────────────────────────
//  Domain types
// ──────────────────────────────────────────────

// PlayerState holds the current state of the music player.
type PlayerState struct {
	Playing     bool    `json:"playing"`
	Paused      bool    `json:"paused"`
	Loop        bool    `json:"loop"`
	Volume      int     `json:"volume"`
	CurrentSong string  `json:"currentSong"`
	Position    float64 `json:"position"`
	Duration    float64 `json:"duration"`
}

// PlaylistSong represents a single song entry inside a playlist.
type PlaylistSong struct {
	Name string `json:"name"`
}

// Playlist is a named, ordered collection of songs with a unique ID.
type Playlist struct {
	ID          int            `json:"id"`
	Name        string         `json:"name"`
	Songs       []PlaylistSong `json:"songs"`
	AlbumArt    string         `json:"albumArt"`    // base64 data URL for custom album art
	IsFavorites bool           `json:"isFavorites"` // true for the special Favorites playlist
}

// PlaylistState is the complete snapshot sent to the frontend over the
// "playlist" event.
type PlaylistState struct {
	Playlists  []Playlist `json:"playlists"`
	ActivePL   int        `json:"activePL"`   // slice index, -1 = none
	ActiveSong int        `json:"activeSong"` // index within the active playlist
}

// ──────────────────────────────────────────────
//  App struct
// ──────────────────────────────────────────────

// App is the main application struct bound to the Wails frontend.
type App struct {
	// ctx is the Wails runtime context, set in startup().
	ctx context.Context

	// pipeSeq is incremented atomically to produce a unique named-pipe path
	// for every mpv session, preventing conflicts between the outgoing and
	// incoming sessions during a rapid Stop→Play transition.
	pipeSeq uint64

	// ipcMu guards conn and writer.
	ipcMu  sync.Mutex
	conn   net.Conn
	writer *bufio.Writer

	// stateMu guards state.
	stateMu sync.RWMutex
	state   PlayerState

	// cmdMu guards cmd and cancelCtx.
	cmdMu     sync.Mutex
	cmd       *exec.Cmd
	cancelCtx context.CancelFunc

	// plMu guards all playlist fields below.
	plMu       sync.Mutex
	playlists  []Playlist
	activePL   int // slice index into playlists, -1 = no playlist playing
	activeSong int // index of the currently playing song within activePL
	nextPLID   int // monotonically increasing ID assigned to new playlists
}

// NewApp creates and returns a new App with sensible defaults.
func NewApp() *App {
	return &App{
		state:    PlayerState{Volume: 100},
		activePL: -1,
	}
}

// startup is called by Wails when the application starts.
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	a.loadPlaylists()
	a.ensureFavoritesPlaylist()
}

// shutdown is called by Wails when the application is closing.
func (a *App) shutdown(ctx context.Context) {
	a.Stop()
}

// ──────────────────────────────────────────────
//  Exported player API
// ──────────────────────────────────────────────

// PlaySong stops any current playback and launches mpv to search for and play
// the given song name via yt-dlp. Returns "" on success or an error string.
func (a *App) PlaySong(name string) string {
	// User-initiated: disassociate from any active playlist.
	a.plMu.Lock()
	a.activePL = -1
	a.plMu.Unlock()

	return a.startPlayback(name)
}

// Stop halts playback, tears down the IPC connection, resets transient player
// state, and clears the active playlist association.
func (a *App) Stop() {
	a.plMu.Lock()
	a.activePL = -1
	a.plMu.Unlock()

	a.stopInternal()
	a.emitPlaylistState()
}

// TogglePause cycles the pause state in mpv.
func (a *App) TogglePause() string {
	a.stateMu.RLock()
	playing := a.state.Playing
	a.stateMu.RUnlock()

	if !playing {
		return "not playing"
	}
	if err := a.sendCommand(map[string]interface{}{
		"command": []interface{}{"cycle", "pause"},
	}); err != nil {
		return err.Error()
	}
	return ""
}

// SeekForward skips ahead 10 seconds.
func (a *App) SeekForward() string {
	if err := a.sendCommand(map[string]interface{}{
		"command": []interface{}{"seek", 10, "relative"},
	}); err != nil {
		return err.Error()
	}
	return ""
}

// SeekBackward rewinds 10 seconds.
func (a *App) SeekBackward() string {
	if err := a.sendCommand(map[string]interface{}{
		"command": []interface{}{"seek", -10, "relative"},
	}); err != nil {
		return err.Error()
	}
	return ""
}

// SetVolume sets mpv's playback volume (0–130).
func (a *App) SetVolume(vol int) string {
	if vol < 0 || vol > 130 {
		return "volume must be between 0 and 130"
	}

	a.stateMu.Lock()
	a.state.Volume = vol
	a.stateMu.Unlock()

	if err := a.sendCommand(map[string]interface{}{
		"command": []interface{}{"set_property", "volume", vol},
	}); err != nil {
		return err.Error()
	}
	return ""
}

// ToggleLoop toggles single-file looping in mpv.
func (a *App) ToggleLoop() string {
	a.stateMu.RLock()
	loop := a.state.Loop
	a.stateMu.RUnlock()

	loopValue := "inf"
	if loop {
		loopValue = "no"
	}

	if err := a.sendCommand(map[string]interface{}{
		"command": []interface{}{"set_property", "loop-file", loopValue},
	}); err != nil {
		return err.Error()
	}
	return ""
}

// SeekTo seeks to an absolute position in seconds.
func (a *App) SeekTo(position float64) string {
	if err := a.sendCommand(map[string]interface{}{
		"command": []interface{}{"seek", position, "absolute"},
	}); err != nil {
		return err.Error()
	}
	return ""
}

// GetState returns a snapshot of the current PlayerState.
func (a *App) GetState() PlayerState {
	a.stateMu.RLock()
	defer a.stateMu.RUnlock()
	return a.state
}

// ──────────────────────────────────────────────
//  Exported playlist API
// ──────────────────────────────────────────────

// CreatePlaylist appends a new empty playlist and returns the updated
// PlaylistState.
func (a *App) CreatePlaylist(name string) PlaylistState {
	a.plMu.Lock()
	a.nextPLID++
	pl := Playlist{
		ID:    a.nextPLID,
		Name:  name,
		Songs: []PlaylistSong{},
	}
	a.playlists = append(a.playlists, pl)
	a.plMu.Unlock()

	a.savePlaylists()
	a.emitPlaylistState()
	return a.GetPlaylistState()
}

// DeletePlaylist removes the playlist with the given ID. If it was the active
// playlist, playback is stopped and activePL is reset to -1.
// loadPlaylists reads the persisted playlist JSON from the config folder and
// restores it into memory. Called once on startup. Errors are silently ignored
// (e.g. first run where the file does not yet exist).
func (a *App) loadPlaylists() {
	path := configpath.GetConfigPath(playlistsFile)
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var playlists []Playlist
	if err := json.Unmarshal(data, &playlists); err != nil {
		return
	}
	a.plMu.Lock()
	a.playlists = playlists
	// Advance nextPLID past the highest ID that was loaded so new playlists
	// never collide with restored ones.
	for _, pl := range playlists {
		if pl.ID >= a.nextPLID {
			a.nextPLID = pl.ID + 1
		}
	}
	a.plMu.Unlock()
}

// savePlaylists serialises the current playlist slice to JSON and writes it to
// the config folder. Errors are silently ignored.
func (a *App) savePlaylists() {
	configpath.CreateConfigPath(playlistsFile)
	path := configpath.GetConfigPath(playlistsFile)
	a.plMu.Lock()
	data, err := json.MarshalIndent(a.playlists, "", "  ")
	a.plMu.Unlock()
	if err != nil {
		return
	}
	_ = os.WriteFile(path, data, 0644)
}

func (a *App) DeletePlaylist(id int) PlaylistState {
	var wasActive bool

	a.plMu.Lock()
	idx := a.findPlaylistIdx(id)
	if idx >= 0 {
		a.playlists = append(a.playlists[:idx], a.playlists[idx+1:]...)
		if a.activePL == idx {
			a.activePL = -1
			wasActive = true
		} else if a.activePL > idx {
			// Shift the pointer down to keep it pointing at the same playlist.
			a.activePL--
		}
	}
	a.plMu.Unlock()

	if wasActive {
		a.stopInternal()
	}
	a.savePlaylists()
	a.emitPlaylistState()
	return a.GetPlaylistState()
}

// AddSongToPlaylist appends a song to the playlist with the given ID.
func (a *App) AddSongToPlaylist(plID int, name string) PlaylistState {
	a.plMu.Lock()
	idx := a.findPlaylistIdx(plID)
	if idx >= 0 {
		a.playlists[idx].Songs = append(
			a.playlists[idx].Songs,
			PlaylistSong{Name: name},
		)
	}
	a.plMu.Unlock()

	a.savePlaylists()
	a.emitPlaylistState()
	return a.GetPlaylistState()
}

// RemoveSongFromPlaylist removes the song at songIdx from the playlist with
// the given ID.
func (a *App) RemoveSongFromPlaylist(plID int, songIdx int) PlaylistState {
	a.plMu.Lock()
	idx := a.findPlaylistIdx(plID)
	if idx >= 0 {
		songs := a.playlists[idx].Songs
		if songIdx >= 0 && songIdx < len(songs) {
			a.playlists[idx].Songs = append(songs[:songIdx], songs[songIdx+1:]...)
		}
	}
	a.plMu.Unlock()

	a.savePlaylists()
	a.emitPlaylistState()
	return a.GetPlaylistState()
}

// ReorderSong moves the song at fromIdx to toIdx within the playlist identified
// by plID. Both indices are 0-based. If either index is out of range, or they
// are equal, the call is a no-op.
func (a *App) ReorderSong(plID int, fromIdx int, toIdx int) PlaylistState {
	a.plMu.Lock()
	idx := a.findPlaylistIdx(plID)
	if idx >= 0 {
		songs := a.playlists[idx].Songs
		if fromIdx >= 0 && fromIdx < len(songs) &&
			toIdx >= 0 && toIdx < len(songs) &&
			fromIdx != toIdx {

			song := songs[fromIdx]

			// Remove from original position.
			songs = append(songs[:fromIdx], songs[fromIdx+1:]...)

			// When moving forward, the target shifts left by one because of
			// the removal above.
			if fromIdx < toIdx {
				toIdx--
			}

			// Insert at the new position.
			songs = append(songs[:toIdx],
				append([]PlaylistSong{song}, songs[toIdx:]...)...)

			a.playlists[idx].Songs = songs
		}
	}
	a.plMu.Unlock()

	a.savePlaylists()
	a.emitPlaylistState()
	return a.GetPlaylistState()
}

// GetPlaylistState returns a deep copy of the current playlist state so the
// caller cannot mutate internal slice state.
func (a *App) GetPlaylistState() PlaylistState {
	a.plMu.Lock()
	defer a.plMu.Unlock()

	pls := make([]Playlist, len(a.playlists))
	for i, pl := range a.playlists {
		songs := make([]PlaylistSong, len(pl.Songs))
		copy(songs, pl.Songs)
		pls[i] = Playlist{
			ID:          pl.ID,
			Name:        pl.Name,
			Songs:       songs,
			AlbumArt:    pl.AlbumArt,
			IsFavorites: pl.IsFavorites,
		}
	}
	return PlaylistState{
		Playlists:  pls,
		ActivePL:   a.activePL,
		ActiveSong: a.activeSong,
	}
}

// SetPlaylistAlbumArt stores a base64-encoded data URL as the album art for
// the given playlist ID. Pass an empty string to clear the art.
func (a *App) SetPlaylistAlbumArt(id int, dataURL string) PlaylistState {
	a.plMu.Lock()
	idx := a.findPlaylistIdx(id)
	if idx >= 0 {
		a.playlists[idx].AlbumArt = dataURL
	}
	a.plMu.Unlock()
	a.savePlaylists()
	a.emitPlaylistState()
	return a.GetPlaylistState()
}

// ensureFavoritesPlaylist creates the built-in Favorites playlist if it does
// not already exist, placing it at the front of the list.
func (a *App) ensureFavoritesPlaylist() {
	a.plMu.Lock()
	for _, pl := range a.playlists {
		if pl.IsFavorites {
			a.plMu.Unlock()
			return
		}
	}
	a.nextPLID++
	fav := Playlist{
		ID:          a.nextPLID,
		Name:        "Favorites",
		Songs:       []PlaylistSong{},
		IsFavorites: true,
	}
	a.playlists = append([]Playlist{fav}, a.playlists...)
	a.plMu.Unlock()
	a.savePlaylists()
}

// ImportExportify parses the content of an Exportify CSV file and appends the
// tracks to the playlist with the given ID.
//
// Exportify columns detected by header name (robust to optional extra columns):
//
//	"Track Name"    – the song title
//	"Artist Name(s)" – comma-separated artist names
//
// Each track is stored as "Artist Name(s) - Track Name" so yt-dlp can find it.
func (a *App) ImportExportify(plID int, csvContent string) PlaylistState {
	songs, err := parseExportifyCSV(csvContent)
	if err != nil || len(songs) == 0 {
		return a.GetPlaylistState()
	}

	a.plMu.Lock()
	idx := a.findPlaylistIdx(plID)
	if idx >= 0 {
		a.playlists[idx].Songs = append(a.playlists[idx].Songs, songs...)
	}
	a.plMu.Unlock()

	a.savePlaylists()
	a.emitPlaylistState()
	return a.GetPlaylistState()
}

// CreatePlaylistFromExportify creates a new playlist with the given name and
// fills it with tracks parsed from an Exportify CSV.
func (a *App) CreatePlaylistFromExportify(name string, csvContent string) PlaylistState {
	songs, err := parseExportifyCSV(csvContent)
	if err != nil {
		return a.GetPlaylistState()
	}

	a.plMu.Lock()
	a.nextPLID++
	pl := Playlist{
		ID:    a.nextPLID,
		Name:  name,
		Songs: songs,
	}
	a.playlists = append(a.playlists, pl)
	a.plMu.Unlock()

	a.savePlaylists()
	a.emitPlaylistState()
	return a.GetPlaylistState()
}

// parseExportifyCSV parses an Exportify-format CSV string and returns a slice
// of PlaylistSong values. Column positions are inferred from the header row so
// the function is resilient to optional extra columns being present.
func parseExportifyCSV(csvContent string) ([]PlaylistSong, error) {
	r := csv.NewReader(strings.NewReader(csvContent))
	r.LazyQuotes = true
	rows, err := r.ReadAll()
	if err != nil || len(rows) < 2 {
		return nil, err
	}

	// Locate the columns we care about by header name.
	trackCol, artistCol := -1, -1
	for i, h := range rows[0] {
		switch strings.TrimSpace(h) {
		case "Track Name":
			trackCol = i
		case "Artist Name(s)":
			artistCol = i
		}
	}
	if trackCol < 0 {
		return nil, fmt.Errorf("no 'Track Name' column found")
	}

	var songs []PlaylistSong
	for _, row := range rows[1:] {
		if trackCol >= len(row) {
			continue
		}
		track := strings.TrimSpace(row[trackCol])
		if track == "" {
			continue
		}
		name := track
		if artistCol >= 0 && artistCol < len(row) {
			if artist := strings.TrimSpace(row[artistCol]); artist != "" {
				name = artist + " - " + track
			}
		}
		songs = append(songs, PlaylistSong{Name: name})
	}
	return songs, nil
}

// PlayPlaylist starts playback of the playlist with the given ID at songIdx.
func (a *App) PlayPlaylist(plID int, songIdx int) string {
	a.plMu.Lock()
	idx := a.findPlaylistIdx(plID)
	if idx < 0 {
		a.plMu.Unlock()
		return fmt.Sprintf("playlist %d not found", plID)
	}
	songs := a.playlists[idx].Songs
	if songIdx < 0 || songIdx >= len(songs) {
		a.plMu.Unlock()
		return fmt.Sprintf("song index %d out of range", songIdx)
	}
	a.activePL = idx
	a.activeSong = songIdx
	name := songs[songIdx].Name
	a.plMu.Unlock()

	result := a.startPlayback(name)
	a.emitPlaylistState()
	return result
}

// NextSong advances to the next song in the active playlist. If there is no
// next song, playback stops and the active playlist is cleared.
func (a *App) NextSong() string {
	a.plMu.Lock()
	if a.activePL < 0 || a.activePL >= len(a.playlists) {
		a.plMu.Unlock()
		return "no active playlist"
	}
	a.activeSong++
	songs := a.playlists[a.activePL].Songs
	if a.activeSong >= len(songs) {
		a.activePL = -1
		a.plMu.Unlock()
		a.stopInternal()
		a.emitPlaylistState()
		return ""
	}
	name := songs[a.activeSong].Name
	a.plMu.Unlock()

	result := a.startPlayback(name)
	a.emitPlaylistState()
	return result
}

// PrevSong moves back to the previous song in the active playlist (floored at
// index 0) and resumes playback.
func (a *App) PrevSong() string {
	a.plMu.Lock()
	if a.activePL < 0 || a.activePL >= len(a.playlists) {
		a.plMu.Unlock()
		return "no active playlist"
	}
	songs := a.playlists[a.activePL].Songs
	if len(songs) == 0 {
		a.plMu.Unlock()
		return "playlist is empty"
	}
	if a.activeSong > 0 {
		a.activeSong--
	}
	name := songs[a.activeSong].Name
	a.plMu.Unlock()

	result := a.startPlayback(name)
	a.emitPlaylistState()
	return result
}

// ──────────────────────────────────────────────
//  Internal helpers
// ──────────────────────────────────────────────

// findPlaylistIdx returns the slice index of the playlist with id, or -1.
// Caller must hold plMu.
func (a *App) findPlaylistIdx(id int) int {
	for i, pl := range a.playlists {
		if pl.ID == id {
			return i
		}
	}
	return -1
}

// stopInternal kills the running mpv process, closes the IPC connection, and
// waits for the OS to fully reclaim the process (and therefore its named-pipe
// server) before returning. It resets transient player state but does NOT
// touch playlist fields.
func (a *App) stopInternal() {
	// Grab and clear the command/cancel under the lock so concurrent calls are
	// safe.
	a.cmdMu.Lock()
	cancel := a.cancelCtx
	cmd := a.cmd
	a.cancelCtx = nil
	a.cmd = nil
	a.cmdMu.Unlock()

	// Send a graceful quit command BEFORE doing anything else. This tells mpv
	// to stop audio output and flush its buffers cleanly. Without this the OS
	// audio subsystem keeps draining whatever mpv already pushed into it, which
	// causes the song to audibly continue for a second or two after Kill().
	// sendCommand is safe here: it acquires ipcMu internally and returns an
	// error (ignored) if the writer is already nil.
	_ = a.sendCommand(map[string]interface{}{"command": []interface{}{"quit"}})

	// Cancel the context. exec.CommandContext also attempts Process.Kill via
	// this path, which is a no-op if mpv already exited from the quit command.
	if cancel != nil {
		cancel()
	}

	// Close the IPC connection so any blocked ReadString in readLoop returns
	// an error and the goroutine exits cleanly.
	a.ipcMu.Lock()
	if a.conn != nil {
		_ = a.conn.Close()
		a.conn = nil
	}
	a.writer = nil
	a.ipcMu.Unlock()

	// Wait for the process to fully exit so the OS reclaims its named-pipe
	// server handle. We do this in a goroutine so we can impose a hard 1 s
	// deadline: if mpv did not honour the quit command in time, force-kill it.
	if cmd != nil && cmd.Process != nil {
		exited := make(chan struct{})
		go func() {
			_ = cmd.Wait()
			close(exited)
		}()
		select {
		case <-exited:
			// mpv exited cleanly via the quit command.
		case <-time.After(time.Second):
			// mpv is taking too long — terminate it forcefully.
			_ = cmd.Process.Kill()
			<-exited
		}
	}

	a.stateMu.Lock()
	a.state.Playing = false
	a.state.Paused = false
	a.state.Position = 0
	a.stateMu.Unlock()

	a.emitState()
}

// startPlayback tears down any existing mpv session and starts a new one for
// name. It does NOT touch playlist fields — callers are responsible for
// updating activePL / activeSong before calling this. Returns "" on success or
// an error string on failure.
func (a *App) startPlayback(name string) string {
	// Always start from a clean slate.
	a.stopInternal()

	a.stateMu.RLock()
	vol := a.state.Volume
	loop := a.state.Loop
	a.stateMu.RUnlock()

	if vol == 0 {
		vol = 100
	}

	// Each session gets its own uniquely named pipe so a slow-dying previous
	// mpv process can never block the incoming one from claiming its pipe.
	seq := atomic.AddUint64(&a.pipeSeq, 1)
	pipe := fmt.Sprintf(`\\.\pipe\moody_mpv_%d`, seq)

	args := []string{
		"--no-video",
		"--af=loudnorm",
		fmt.Sprintf("--volume=%d", vol),
		"--input-ipc-server=" + pipe,
	}
	if loop {
		args = append(args, "--loop-file=inf")
	}
	args = append(args, "ytdl://ytsearch:"+name)

	ctx, cancel := context.WithCancel(context.Background())

	cmd := exec.CommandContext(ctx, "mpv", args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}

	a.cmdMu.Lock()
	a.cmd = cmd
	a.cancelCtx = cancel
	a.cmdMu.Unlock()

	if err := cmd.Start(); err != nil {
		cancel()
		a.cmdMu.Lock()
		a.cmd = nil
		a.cancelCtx = nil
		a.cmdMu.Unlock()
		return fmt.Sprintf("failed to start mpv: %v", err)
	}

	// Connect to the IPC pipe. Retry up to 20 times with 500 ms between
	// attempts. Honour ctx.Done() so a Stop() during the connect phase aborts
	// cleanly without leaving a zombie goroutine.
	dialTimeout := 2 * time.Second
	var conn net.Conn
	var connErr error
	for i := 0; i < 20; i++ {
		select {
		case <-ctx.Done():
			return "cancelled"
		case <-time.After(500 * time.Millisecond):
		}
		conn, connErr = winio.DialPipe(pipe, &dialTimeout)
		if connErr == nil {
			break
		}
	}
	if connErr != nil {
		a.stopInternal()
		return fmt.Sprintf("failed to connect to mpv IPC: %v", connErr)
	}

	a.ipcMu.Lock()
	a.conn = conn
	a.writer = bufio.NewWriter(conn)
	a.ipcMu.Unlock()

	// Seed state optimistically; the real values arrive as property-change
	// events very shortly after.
	a.stateMu.Lock()
	a.state.CurrentSong = name
	a.state.Playing = true
	a.state.Paused = false
	a.state.Position = 0
	a.state.Duration = 0
	a.stateMu.Unlock()

	// Subscribe to the six properties we care about (IDs 1–6).
	observeProps := []string{
		"time-pos",    // 1 → Position
		"pause",       // 2 → Paused
		"volume",      // 3 → Volume
		"duration",    // 4 → Duration
		"loop-file",   // 5 → Loop
		"media-title", // 6 → CurrentSong
	}
	for i, prop := range observeProps {
		_ = a.sendCommand(map[string]interface{}{
			"command": []interface{}{"observe_property", i + 1, prop},
		})
	}

	go a.readLoop(ctx, conn)

	a.emitState()
	a.emitPlaylistState()
	return ""
}

// sendCommand marshals payload as newline-terminated JSON and writes it to the
// IPC pipe. Safe to call concurrently.
func (a *App) sendCommand(payload interface{}) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	data = append(data, '\n')

	a.ipcMu.Lock()
	defer a.ipcMu.Unlock()

	if a.writer == nil {
		return fmt.Errorf("not connected to mpv IPC")
	}
	if _, err = a.writer.Write(data); err != nil {
		return err
	}
	return a.writer.Flush()
}

// readLoop reads newline-delimited JSON events from the IPC connection and
// dispatches them. It exits when conn is closed (either by stopInternal or by
// context cancellation). On end-file it auto-advances the active playlist.
func (a *App) readLoop(ctx context.Context, conn net.Conn) {
	// Close the connection when the context is cancelled so that the blocking
	// ReadString below returns immediately with an error instead of hanging.
	go func() {
		<-ctx.Done()
		conn.Close()
	}()

	reader := bufio.NewReader(conn)
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			// conn was closed or errored — exit cleanly.
			return
		}

		var msg struct {
			Event  string          `json:"event"`
			ID     int             `json:"id"`
			Data   json.RawMessage `json:"data"`
			Reason string          `json:"reason"`
		}
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			continue
		}

		switch msg.Event {
		case "property-change":
			if len(msg.Data) > 0 && string(msg.Data) != "null" {
				a.applyProperty(msg.ID, msg.Data)
			}

		case "start-file":
			a.stateMu.Lock()
			a.state.Playing = true
			a.stateMu.Unlock()
			a.emitState()

		case "end-file":
			// mpv fires end-file with reason "redirect" every time ytdl
			// resolves ytdl://ytsearch:… to the real stream URL. That redirect
			// is not the song finishing — the player is about to start the
			// actual stream immediately. Treating it as EOF would skip to the
			// next playlist song before the current one even begins playing.
			// Only act when the reason is a genuine end-of-file.
			if msg.Reason != "eof" {
				break
			}

			a.stateMu.Lock()
			a.state.Playing = false
			a.state.Paused = false
			a.state.Position = 0
			a.stateMu.Unlock()
			a.emitState()

			// Auto-advance the playlist when one is active.
			a.plMu.Lock()
			if a.activePL >= 0 && a.activePL < len(a.playlists) {
				a.activeSong++
				songs := a.playlists[a.activePL].Songs
				if a.activeSong < len(songs) {
					name := songs[a.activeSong].Name
					a.plMu.Unlock()
					// Run in a new goroutine: startPlayback calls stopInternal
					// which would deadlock if we tried to join this same goroutine.
					go a.startPlayback(name)
				} else {
					// Reached the end of the playlist.
					a.activePL = -1
					a.plMu.Unlock()
					a.emitPlaylistState()
				}
			} else {
				a.plMu.Unlock()
			}
		}
	}
}

// applyProperty updates the relevant PlayerState field for the observed
// property ID and emits a state event to the frontend.
func (a *App) applyProperty(id int, data json.RawMessage) {
	a.stateMu.Lock()
	switch id {
	case 1: // time-pos → Position
		var v float64
		if err := json.Unmarshal(data, &v); err == nil {
			a.state.Position = v
		}

	case 2: // pause → Paused
		var v bool
		if err := json.Unmarshal(data, &v); err == nil {
			a.state.Paused = v
		}

	case 3: // volume → Volume (mpv reports as float)
		var v float64
		if err := json.Unmarshal(data, &v); err == nil {
			a.state.Volume = int(v)
		}

	case 4: // duration → Duration
		var v float64
		if err := json.Unmarshal(data, &v); err == nil {
			a.state.Duration = v
		}

	case 5: // loop-file → Loop
		// mpv sends "inf" (looping) or the boolean false (not looping).
		var s string
		if err := json.Unmarshal(data, &s); err == nil {
			a.state.Loop = s == "inf"
		} else {
			var b bool
			if err := json.Unmarshal(data, &b); err == nil {
				a.state.Loop = b
			}
		}

	case 6: // media-title → CurrentSong
		var s string
		if err := json.Unmarshal(data, &s); err == nil {
			a.state.CurrentSong = s
		}
	}
	a.stateMu.Unlock()

	a.emitState()
}

// emitState broadcasts the current PlayerState to the frontend via the "state"
// Wails event. Safe to call before startup (ctx == nil) — it is a no-op then.
func (a *App) emitState() {
	if a.ctx == nil {
		return
	}
	a.stateMu.RLock()
	state := a.state
	a.stateMu.RUnlock()
	runtime.EventsEmit(a.ctx, "state", state)
}

// emitPlaylistState broadcasts the current PlaylistState to the frontend via
// the "playlist" Wails event. Safe to call before startup — no-op then.
func (a *App) emitPlaylistState() {
	if a.ctx == nil {
		return
	}
	ps := a.GetPlaylistState()
	runtime.EventsEmit(a.ctx, "playlist", ps)
}
