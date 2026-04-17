export namespace main {
	
	export class PlayerState {
	    playing: boolean;
	    paused: boolean;
	    loop: boolean;
	    volume: number;
	    currentSong: string;
	    position: number;
	    duration: number;
	
	    static createFrom(source: any = {}) {
	        return new PlayerState(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.playing = source["playing"];
	        this.paused = source["paused"];
	        this.loop = source["loop"];
	        this.volume = source["volume"];
	        this.currentSong = source["currentSong"];
	        this.position = source["position"];
	        this.duration = source["duration"];
	    }
	}
	export class PlaylistSong {
	    name: string;
	
	    static createFrom(source: any = {}) {
	        return new PlaylistSong(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	    }
	}
	export class Playlist {
	    id: number;
	    name: string;
	    songs: PlaylistSong[];
	    albumArt: string;
	    isFavorites: boolean;
	
	    static createFrom(source: any = {}) {
	        return new Playlist(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.songs = this.convertValues(source["songs"], PlaylistSong);
	        this.albumArt = source["albumArt"];
	        this.isFavorites = source["isFavorites"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	export class PlaylistState {
	    playlists: Playlist[];
	    activePL: number;
	    activeSong: number;
	
	    static createFrom(source: any = {}) {
	        return new PlaylistState(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.playlists = this.convertValues(source["playlists"], Playlist);
	        this.activePL = source["activePL"];
	        this.activeSong = source["activeSong"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

