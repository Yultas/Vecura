export namespace api {
	
	export class AddModelConfig {
	    provider: string;
	    baseUrl: string;
	    apiKey: string;
	    model: string;
	    dim: number;
	    batch: number;
	
	    static createFrom(source: any = {}) {
	        return new AddModelConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.provider = source["provider"];
	        this.baseUrl = source["baseUrl"];
	        this.apiKey = source["apiKey"];
	        this.model = source["model"];
	        this.dim = source["dim"];
	        this.batch = source["batch"];
	    }
	}
	export class GPUInfoResult {
	    vendor: string;
	    name: string;
	    vram: number;
	
	    static createFrom(source: any = {}) {
	        return new GPUInfoResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.vendor = source["vendor"];
	        this.name = source["name"];
	        this.vram = source["vram"];
	    }
	}
	export class LLamaStatus {
	    installed: boolean;
	    version: string;
	    backend: string;
	    serverOK: boolean;
	
	    static createFrom(source: any = {}) {
	        return new LLamaStatus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.installed = source["installed"];
	        this.version = source["version"];
	        this.backend = source["backend"];
	        this.serverOK = source["serverOK"];
	    }
	}
	export class ModelInfo {
	    key: string;
	    provider: string;
	    modelId: string;
	    local: boolean;
	    dim: number;
	
	    static createFrom(source: any = {}) {
	        return new ModelInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.key = source["key"];
	        this.provider = source["provider"];
	        this.modelId = source["modelId"];
	        this.local = source["local"];
	        this.dim = source["dim"];
	    }
	}
	export class PresetModel {
	    id: string;
	    dim: number;
	
	    static createFrom(source: any = {}) {
	        return new PresetModel(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.dim = source["dim"];
	    }
	}
	export class ProviderPreset {
	    id: string;
	    name: string;
	    baseUrl: string;
	    docUrl: string;
	    models: PresetModel[];
	    needsKey: boolean;
	    envKey: string;
	    keyFromEnv: string;
	
	    static createFrom(source: any = {}) {
	        return new ProviderPreset(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.baseUrl = source["baseUrl"];
	        this.docUrl = source["docUrl"];
	        this.models = this.convertValues(source["models"], PresetModel);
	        this.needsKey = source["needsKey"];
	        this.envKey = source["envKey"];
	        this.keyFromEnv = source["keyFromEnv"];
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
	export class RemoteModel {
	    id: string;
	    contextLength: number;
	    isEmbed: boolean;
	
	    static createFrom(source: any = {}) {
	        return new RemoteModel(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.contextLength = source["contextLength"];
	        this.isEmbed = source["isEmbed"];
	    }
	}
	export class SaveSettingsReq {
	    provider: string;
	    baseUrl: string;
	    apiKey: string;
	    selectedModel: string;
	    fetchedModels: RemoteModel[];
	    folderPath: string;
	    providerChecked: boolean;
	
	    static createFrom(source: any = {}) {
	        return new SaveSettingsReq(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.provider = source["provider"];
	        this.baseUrl = source["baseUrl"];
	        this.apiKey = source["apiKey"];
	        this.selectedModel = source["selectedModel"];
	        this.fetchedModels = this.convertValues(source["fetchedModels"], RemoteModel);
	        this.folderPath = source["folderPath"];
	        this.providerChecked = source["providerChecked"];
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
	export class SearchHit {
	    id: number;
	    path: string;
	    prompt: string;
	    score: number;
	    date: number;
	
	    static createFrom(source: any = {}) {
	        return new SearchHit(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.path = source["path"];
	        this.prompt = source["prompt"];
	        this.score = source["score"];
	        this.date = source["date"];
	    }
	}
	export class UpdateInfo {
	    available: boolean;
	    current: string;
	    version: string;
	    url: string;
	    sha256: string;
	    signature: string;
	    notes: string;
	
	    static createFrom(source: any = {}) {
	        return new UpdateInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.available = source["available"];
	        this.current = source["current"];
	        this.version = source["version"];
	        this.url = source["url"];
	        this.sha256 = source["sha256"];
	        this.signature = source["signature"];
	        this.notes = source["notes"];
	    }
	}
	export class modelCfg {
	    provider: string;
	    baseUrl: string;
	    apiKey: string;
	    model: string;
	    dim: number;
	
	    static createFrom(source: any = {}) {
	        return new modelCfg(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.provider = source["provider"];
	        this.baseUrl = source["baseUrl"];
	        this.apiKey = source["apiKey"];
	        this.model = source["model"];
	        this.dim = source["dim"];
	    }
	}
	export class providerCfg {
	    baseUrl: string;
	    apiKey: string;
	
	    static createFrom(source: any = {}) {
	        return new providerCfg(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.baseUrl = source["baseUrl"];
	        this.apiKey = source["apiKey"];
	    }
	}
	export class appConfig {
	    provider: string;
	    providers: Record<string, providerCfg>;
	    providerChecks: Record<string, boolean>;
	    selectedModel: string;
	    fetchedModels: RemoteModel[];
	    activeModel: string;
	    folderPath: string;
	    models: modelCfg[];
	    llamaBackend: string;
	    autoStartLocal: boolean;
	    localModelPath: string;
	    localModelDim: number;
	    lastUpdateCheck: number;
	
	    static createFrom(source: any = {}) {
	        return new appConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.provider = source["provider"];
	        this.providers = this.convertValues(source["providers"], providerCfg, true);
	        this.providerChecks = source["providerChecks"];
	        this.selectedModel = source["selectedModel"];
	        this.fetchedModels = this.convertValues(source["fetchedModels"], RemoteModel);
	        this.activeModel = source["activeModel"];
	        this.folderPath = source["folderPath"];
	        this.models = this.convertValues(source["models"], modelCfg);
	        this.llamaBackend = source["llamaBackend"];
	        this.autoStartLocal = source["autoStartLocal"];
	        this.localModelPath = source["localModelPath"];
	        this.localModelDim = source["localModelDim"];
	        this.lastUpdateCheck = source["lastUpdateCheck"];
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

