export namespace app {
	
	export class ConnInfo {
	    context: string;
	    server: string;
	    namespace: string;
	    project: string;
	    admin: boolean;
	    clusterAdmin: boolean;
	    forgeai: boolean;
	    sandboxes: boolean;
	
	    static createFrom(source: any = {}) {
	        return new ConnInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.context = source["context"];
	        this.server = source["server"];
	        this.namespace = source["namespace"];
	        this.project = source["project"];
	        this.admin = source["admin"];
	        this.clusterAdmin = source["clusterAdmin"];
	        this.forgeai = source["forgeai"];
	        this.sandboxes = source["sandboxes"];
	    }
	}
	export class Dependency {
	    name: string;
	    title: string;
	    description: string;
	    pinned: string;
	    installed: string;
	    status: string;
	    message: string;
	    installable: boolean;
	
	    static createFrom(source: any = {}) {
	        return new Dependency(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.title = source["title"];
	        this.description = source["description"];
	        this.pinned = source["pinned"];
	        this.installed = source["installed"];
	        this.status = source["status"];
	        this.message = source["message"];
	        this.installable = source["installable"];
	    }
	}
	export class Dependencies {
	    clusterAdmin: boolean;
	    items: Dependency[];
	
	    static createFrom(source: any = {}) {
	        return new Dependencies(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.clusterAdmin = source["clusterAdmin"];
	        this.items = this.convertValues(source["items"], Dependency);
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
	
	export class FileServerInfo {
	    idleTimeout: v1.Duration;
	
	    static createFrom(source: any = {}) {
	        return new FileServerInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.idleTimeout = this.convertValues(source["idleTimeout"], v1.Duration);
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
	export class Files {
	    phase: string;
	    address?: string;
	    volumes: string[];
	
	    static createFrom(source: any = {}) {
	        return new Files(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.phase = source["phase"];
	        this.address = source["address"];
	        this.volumes = source["volumes"];
	    }
	}
	export class GPUUsage {
	    class: string;
	    quota: number;
	    used: number;
	    pods: number;
	
	    static createFrom(source: any = {}) {
	        return new GPUUsage(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.class = source["class"];
	        this.quota = source["quota"];
	        this.used = source["used"];
	        this.pods = source["pods"];
	    }
	}
	export class Image {
	    repository: string;
	    digest: string;
	    tags: string[];
	    sizeMiB: number;
	    pushedAt: string;
	    pulledAt?: string;
	
	    static createFrom(source: any = {}) {
	        return new Image(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.repository = source["repository"];
	        this.digest = source["digest"];
	        this.tags = source["tags"];
	        this.sizeMiB = source["sizeMiB"];
	        this.pushedAt = source["pushedAt"];
	        this.pulledAt = source["pulledAt"];
	    }
	}
	export class ImageRepo {
	    name: string;
	    count: number;
	    pulls: number;
	    images: Image[];
	
	    static createFrom(source: any = {}) {
	        return new ImageRepo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.count = source["count"];
	        this.pulls = source["pulls"];
	        this.images = this.convertValues(source["images"], Image);
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
	export class InstanceType {
	    name: string;
	    class: string;
	    product: string;
	    gpuCount: number;
	    gpuMemoryMiB: number;
	    cpu: string;
	    memory: string;
	    total: number;
	    free: number;
	
	    static createFrom(source: any = {}) {
	        return new InstanceType(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.class = source["class"];
	        this.product = source["product"];
	        this.gpuCount = source["gpuCount"];
	        this.gpuMemoryMiB = source["gpuMemoryMiB"];
	        this.cpu = source["cpu"];
	        this.memory = source["memory"];
	        this.total = source["total"];
	        this.free = source["free"];
	    }
	}
	export class Worker {
	    index: number;
	    podName: string;
	    nodeName: string;
	    phase: string;
	    message?: string;
	
	    static createFrom(source: any = {}) {
	        return new Worker(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.index = source["index"];
	        this.podName = source["podName"];
	        this.nodeName = source["nodeName"];
	        this.phase = source["phase"];
	        this.message = source["message"];
	    }
	}
	export class Job {
	    name: string;
	    group: string;
	    retryOf?: string;
	    template?: string;
	    image: string;
	    command: string;
	    env: Record<string, string>;
	    instanceType: string;
	    workers: number;
	    phase: string;
	    message?: string;
	    failureReason?: string;
	    stopped: boolean;
	    superseded: boolean;
	    ready: number;
	    cpu?: string;
	    gpuClass?: string;
	    gpuCount?: number;
	    created: string;
	    age: string;
	    duration?: string;
	    workerPods: Worker[];
	    volumes?: string[];
	    maxRetries?: number;
	    retryOn?: string;
	    retriedAs?: string;
	    retries: Job[];
	
	    static createFrom(source: any = {}) {
	        return new Job(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.group = source["group"];
	        this.retryOf = source["retryOf"];
	        this.template = source["template"];
	        this.image = source["image"];
	        this.command = source["command"];
	        this.env = source["env"];
	        this.instanceType = source["instanceType"];
	        this.workers = source["workers"];
	        this.phase = source["phase"];
	        this.message = source["message"];
	        this.failureReason = source["failureReason"];
	        this.stopped = source["stopped"];
	        this.superseded = source["superseded"];
	        this.ready = source["ready"];
	        this.cpu = source["cpu"];
	        this.gpuClass = source["gpuClass"];
	        this.gpuCount = source["gpuCount"];
	        this.created = source["created"];
	        this.age = source["age"];
	        this.duration = source["duration"];
	        this.workerPods = this.convertValues(source["workerPods"], Worker);
	        this.volumes = source["volumes"];
	        this.maxRetries = source["maxRetries"];
	        this.retryOn = source["retryOn"];
	        this.retriedAs = source["retriedAs"];
	        this.retries = this.convertValues(source["retries"], Job);
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
	export class KubeContext {
	    name: string;
	    cluster: string;
	    namespace: string;
	    current: boolean;
	
	    static createFrom(source: any = {}) {
	        return new KubeContext(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.cluster = source["cluster"];
	        this.namespace = source["namespace"];
	        this.current = source["current"];
	    }
	}
	export class TrainingInfo {
	    defaultRuntime: string;
	    shmSize: string;
	    stuckTimeout: v1.Duration;
	    maxRetries: number;
	
	    static createFrom(source: any = {}) {
	        return new TrainingInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.defaultRuntime = source["defaultRuntime"];
	        this.shmSize = source["shmSize"];
	        this.stuckTimeout = this.convertValues(source["stuckTimeout"], v1.Duration);
	        this.maxRetries = source["maxRetries"];
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
	export class StorageTypeInfo {
	    storageClass: string;
	    accessMode: string;
	    description: string;
	
	    static createFrom(source: any = {}) {
	        return new StorageTypeInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.storageClass = source["storageClass"];
	        this.accessMode = source["accessMode"];
	        this.description = source["description"];
	    }
	}
	export class RegistryInfo {
	    enabled: boolean;
	    url: string;
	
	    static createFrom(source: any = {}) {
	        return new RegistryInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.enabled = source["enabled"];
	        this.url = source["url"];
	    }
	}
	export class PlatformConfig {
	    apiServer: string;
	    ingress: platform.Ingress;
	    registry: RegistryInfo;
	    storageTypes: Record<string, StorageTypeInfo>;
	    fileServer: FileServerInfo;
	    trash: v1.Duration;
	    training: TrainingInfo;
	
	    static createFrom(source: any = {}) {
	        return new PlatformConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.apiServer = source["apiServer"];
	        this.ingress = this.convertValues(source["ingress"], platform.Ingress);
	        this.registry = this.convertValues(source["registry"], RegistryInfo);
	        this.storageTypes = this.convertValues(source["storageTypes"], StorageTypeInfo, true);
	        this.fileServer = this.convertValues(source["fileServer"], FileServerInfo);
	        this.trash = this.convertValues(source["trash"], v1.Duration);
	        this.training = this.convertValues(source["training"], TrainingInfo);
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
	export class ProjectMember {
	    name: string;
	    role: string;
	
	    static createFrom(source: any = {}) {
	        return new ProjectMember(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.role = source["role"];
	    }
	}
	export class ProjectNetwork {
	    isolation: string;
	    denyInternet: boolean;
	
	    static createFrom(source: any = {}) {
	        return new ProjectNetwork(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.isolation = source["isolation"];
	        this.denyInternet = source["denyInternet"];
	    }
	}
	export class ProjectQuota {
	    gpus?: Record<string, number>;
	    storage?: string;
	    volumes: number;
	    pods: number;
	    registryStorage?: string;
	
	    static createFrom(source: any = {}) {
	        return new ProjectQuota(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.gpus = source["gpus"];
	        this.storage = source["storage"];
	        this.volumes = source["volumes"];
	        this.pods = source["pods"];
	        this.registryStorage = source["registryStorage"];
	    }
	}
	export class Project {
	    name: string;
	    displayName: string;
	    namespace: string;
	    quota: ProjectQuota;
	    network: ProjectNetwork;
	    members: ProjectMember[];
	    trashRetention?: string;
	    deletionPolicy: string;
	    ready: boolean;
	    registry: string;
	    created: string;
	
	    static createFrom(source: any = {}) {
	        return new Project(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.displayName = source["displayName"];
	        this.namespace = source["namespace"];
	        this.quota = this.convertValues(source["quota"], ProjectQuota);
	        this.network = this.convertValues(source["network"], ProjectNetwork);
	        this.members = this.convertValues(source["members"], ProjectMember);
	        this.trashRetention = source["trashRetention"];
	        this.deletionPolicy = source["deletionPolicy"];
	        this.ready = source["ready"];
	        this.registry = source["registry"];
	        this.created = source["created"];
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
	
	export class ProjectNamespace {
	    namespace: string;
	    project: string;
	
	    static createFrom(source: any = {}) {
	        return new ProjectNamespace(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.namespace = source["namespace"];
	        this.project = source["project"];
	    }
	}
	
	
	export class QuotaUsage {
	    gpuClasses: GPUUsage[];
	    storage: string;
	    storageUsed: string;
	    volumes: number;
	    volumeMax: number;
	    pods: number;
	    podMax: number;
	
	    static createFrom(source: any = {}) {
	        return new QuotaUsage(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.gpuClasses = this.convertValues(source["gpuClasses"], GPUUsage);
	        this.storage = source["storage"];
	        this.storageUsed = source["storageUsed"];
	        this.volumes = source["volumes"];
	        this.volumeMax = source["volumeMax"];
	        this.pods = source["pods"];
	        this.podMax = source["podMax"];
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
	export class Registry {
	    enabled: boolean;
	    url: string;
	    username?: string;
	    password?: string;
	    secret: string;
	
	    static createFrom(source: any = {}) {
	        return new Registry(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.enabled = source["enabled"];
	        this.url = source["url"];
	        this.username = source["username"];
	        this.password = source["password"];
	        this.secret = source["secret"];
	    }
	}
	
	export class Sandbox {
	    name: string;
	    phase: string;
	    message?: string;
	    image: string;
	    command?: string;
	    env: Record<string, string>;
	    instanceType?: string;
	    runtimeClass?: string;
	    service: boolean;
	    serviceFQDN?: string;
	    podIPs?: string[];
	    nodeName?: string;
	    podName?: string;
	    podPhase?: string;
	    cpu?: string;
	    memory?: string;
	    gpu?: number;
	    volumes?: string[];
	    stopped: boolean;
	    idleSince?: string;
	    shutdownTime?: string;
	    created: string;
	    age: string;
	
	    static createFrom(source: any = {}) {
	        return new Sandbox(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.phase = source["phase"];
	        this.message = source["message"];
	        this.image = source["image"];
	        this.command = source["command"];
	        this.env = source["env"];
	        this.instanceType = source["instanceType"];
	        this.runtimeClass = source["runtimeClass"];
	        this.service = source["service"];
	        this.serviceFQDN = source["serviceFQDN"];
	        this.podIPs = source["podIPs"];
	        this.nodeName = source["nodeName"];
	        this.podName = source["podName"];
	        this.podPhase = source["podPhase"];
	        this.cpu = source["cpu"];
	        this.memory = source["memory"];
	        this.gpu = source["gpu"];
	        this.volumes = source["volumes"];
	        this.stopped = source["stopped"];
	        this.idleSince = source["idleSince"];
	        this.shutdownTime = source["shutdownTime"];
	        this.created = source["created"];
	        this.age = source["age"];
	    }
	}
	export class SandboxOptions {
	    defaultImage: string;
	    images: string[];
	    runtimeClasses: string[];
	    defaultRuntimeClass: string;
	    idleTimeout?: string;
	
	    static createFrom(source: any = {}) {
	        return new SandboxOptions(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.defaultImage = source["defaultImage"];
	        this.images = source["images"];
	        this.runtimeClasses = source["runtimeClasses"];
	        this.defaultRuntimeClass = source["defaultRuntimeClass"];
	        this.idleTimeout = source["idleTimeout"];
	    }
	}
	export class SandboxTemplate {
	    name: string;
	    description?: string;
	    images?: string[];
	    created: string;
	    form?: sandbox.TemplateForm;
	
	    static createFrom(source: any = {}) {
	        return new SandboxTemplate(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.description = source["description"];
	        this.images = source["images"];
	        this.created = source["created"];
	        this.form = this.convertValues(source["form"], sandbox.TemplateForm);
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
	
	export class Template {
	    name: string;
	    description?: string;
	    form?: training.Form;
	    created: string;
	
	    static createFrom(source: any = {}) {
	        return new Template(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.description = source["description"];
	        this.form = this.convertValues(source["form"], training.Form);
	        this.created = source["created"];
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

export namespace platform {
	
	export class Ingress {
	    className?: string;
	    domain?: string;
	    port?: number;
	    tlsSecret?: string;
	
	    static createFrom(source: any = {}) {
	        return new Ingress(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.className = source["className"];
	        this.domain = source["domain"];
	        this.port = source["port"];
	        this.tlsSecret = source["tlsSecret"];
	    }
	}

}

export namespace sandbox {
	
	export class EnvVar {
	    name: string;
	    value: string;
	
	    static createFrom(source: any = {}) {
	        return new EnvVar(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.value = source["value"];
	    }
	}
	export class VolumeMount {
	    volume: string;
	    mountPath: string;
	    subPath?: string;
	    readOnly?: boolean;
	
	    static createFrom(source: any = {}) {
	        return new VolumeMount(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.volume = source["volume"];
	        this.mountPath = source["mountPath"];
	        this.subPath = source["subPath"];
	        this.readOnly = source["readOnly"];
	    }
	}
	export class Form {
	    name: string;
	    image: string;
	    command?: string;
	    env?: EnvVar[];
	    instanceType?: string;
	    runtimeClass?: string;
	    service?: boolean;
	    volumes?: VolumeMount[];
	    template?: string;
	    ttl: v1.Duration;
	    idleTimeout?: v1.Duration;
	
	    static createFrom(source: any = {}) {
	        return new Form(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.image = source["image"];
	        this.command = source["command"];
	        this.env = this.convertValues(source["env"], EnvVar);
	        this.instanceType = source["instanceType"];
	        this.runtimeClass = source["runtimeClass"];
	        this.service = source["service"];
	        this.volumes = this.convertValues(source["volumes"], VolumeMount);
	        this.template = source["template"];
	        this.ttl = this.convertValues(source["ttl"], v1.Duration);
	        this.idleTimeout = this.convertValues(source["idleTimeout"], v1.Duration);
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
	export class TemplateForm {
	    name: string;
	    description?: string;
	    image: string;
	    command?: string;
	    env?: EnvVar[];
	    runtimeClass?: string;
	
	    static createFrom(source: any = {}) {
	        return new TemplateForm(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.description = source["description"];
	        this.image = source["image"];
	        this.command = source["command"];
	        this.env = this.convertValues(source["env"], EnvVar);
	        this.runtimeClass = source["runtimeClass"];
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

export namespace time {
	
	export class Time {
	
	
	    static createFrom(source: any = {}) {
	        return new Time(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	
	    }
	}

}

export namespace training {
	
	export class EnvVar {
	    name: string;
	    value: string;
	
	    static createFrom(source: any = {}) {
	        return new EnvVar(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.value = source["value"];
	    }
	}
	export class VolumeMount {
	    volume: string;
	    mountPath: string;
	    subPath?: string;
	    readOnly?: boolean;
	
	    static createFrom(source: any = {}) {
	        return new VolumeMount(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.volume = source["volume"];
	        this.mountPath = source["mountPath"];
	        this.subPath = source["subPath"];
	        this.readOnly = source["readOnly"];
	    }
	}
	export class Form {
	    name?: string;
	    template?: string;
	    description?: string;
	    image: string;
	    command?: string;
	    env?: EnvVar[];
	    instanceType: string;
	    workers: number;
	    volumes?: VolumeMount[];
	    maxRetries?: number;
	    retryOn?: string;
	
	    static createFrom(source: any = {}) {
	        return new Form(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.template = source["template"];
	        this.description = source["description"];
	        this.image = source["image"];
	        this.command = source["command"];
	        this.env = this.convertValues(source["env"], EnvVar);
	        this.instanceType = source["instanceType"];
	        this.workers = source["workers"];
	        this.volumes = this.convertValues(source["volumes"], VolumeMount);
	        this.maxRetries = source["maxRetries"];
	        this.retryOn = source["retryOn"];
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

export namespace v1 {
	
	export class Duration {
	    Duration: number;
	
	    static createFrom(source: any = {}) {
	        return new Duration(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Duration = source["Duration"];
	    }
	}

}

export namespace volumes {
	
	export class Volume {
	    name: string;
	    type: string;
	    storageClass: string;
	    phase: string;
	    requestedGiB: number;
	    capacityGiB: number;
	    accessMode: string;
	    created: string;
	    users: string[];
	    inTrash: boolean;
	    trashedAt?: string;
	    expireAt?: string;
	
	    static createFrom(source: any = {}) {
	        return new Volume(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.type = source["type"];
	        this.storageClass = source["storageClass"];
	        this.phase = source["phase"];
	        this.requestedGiB = source["requestedGiB"];
	        this.capacityGiB = source["capacityGiB"];
	        this.accessMode = source["accessMode"];
	        this.created = source["created"];
	        this.users = source["users"];
	        this.inTrash = source["inTrash"];
	        this.trashedAt = source["trashedAt"];
	        this.expireAt = source["expireAt"];
	    }
	}

}

