export namespace jobs {
	
	export class SSHConnection {
	    id: string;
	    url: string;
	    refactorURL: string;
	    password: string;
	
	    static createFrom(source: any = {}) {
	        return new SSHConnection(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.url = source["url"];
	        this.refactorURL = source["refactorURL"];
	        this.password = source["password"];
	    }
	}

}

export namespace main {
	
	export class ConnInfo {
	    context: string;
	    server: string;
	    namespace: string;
	    userID: string;
	
	    static createFrom(source: any = {}) {
	        return new ConnInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.context = source["context"];
	        this.server = source["server"];
	        this.namespace = source["namespace"];
	        this.userID = source["userID"];
	    }
	}
	export class Worker {
	    index: number;
	    podName: string;
	    nodeName: string;
	    podIP: string;
	    phase: string;
	    sshUser: string;
	    jupyterURLs: string[];
	
	    static createFrom(source: any = {}) {
	        return new Worker(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.index = source["index"];
	        this.podName = source["podName"];
	        this.nodeName = source["nodeName"];
	        this.podIP = source["podIP"];
	        this.phase = source["phase"];
	        this.sshUser = source["sshUser"];
	        this.jupyterURLs = source["jupyterURLs"];
	    }
	}
	export class Job {
	    name: string;
	    jobID: string;
	    userID: string;
	    image: string;
	    command: string;
	    resourceID: string;
	    poolID: string;
	    workers: number;
	    env: Record<string, string>;
	    phase: string;
	    message: string;
	    ready: number;
	    succeeded: number;
	    failed: number;
	    created: string;
	    started: string;
	    completed: string;
	    age: string;
	    duration: string;
	    workerPods: Worker[];
	    suspended: boolean;
	    restartGeneration: number;
	    trainJob: string;
	    sshEnabled: boolean;
	    jupyterEnabled: boolean;
	    volumeCount: number;
	
	    static createFrom(source: any = {}) {
	        return new Job(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.jobID = source["jobID"];
	        this.userID = source["userID"];
	        this.image = source["image"];
	        this.command = source["command"];
	        this.resourceID = source["resourceID"];
	        this.poolID = source["poolID"];
	        this.workers = source["workers"];
	        this.env = source["env"];
	        this.phase = source["phase"];
	        this.message = source["message"];
	        this.ready = source["ready"];
	        this.succeeded = source["succeeded"];
	        this.failed = source["failed"];
	        this.created = source["created"];
	        this.started = source["started"];
	        this.completed = source["completed"];
	        this.age = source["age"];
	        this.duration = source["duration"];
	        this.workerPods = this.convertValues(source["workerPods"], Worker);
	        this.suspended = source["suspended"];
	        this.restartGeneration = source["restartGeneration"];
	        this.trainJob = source["trainJob"];
	        this.sshEnabled = source["sshEnabled"];
	        this.jupyterEnabled = source["jupyterEnabled"];
	        this.volumeCount = source["volumeCount"];
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
	export class JobForm {
	    jobID: string;
	    userID: string;
	    image: string;
	    command: string;
	    resourceID: string;
	    poolID: string;
	    workers: number;
	    env: Record<string, string>;
	    ssh: boolean;
	    jupyter: boolean;
	
	    static createFrom(source: any = {}) {
	        return new JobForm(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.jobID = source["jobID"];
	        this.userID = source["userID"];
	        this.image = source["image"];
	        this.command = source["command"];
	        this.resourceID = source["resourceID"];
	        this.poolID = source["poolID"];
	        this.workers = source["workers"];
	        this.env = source["env"];
	        this.ssh = source["ssh"];
	        this.jupyter = source["jupyter"];
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
	export class ResourceInfo {
	    id: string;
	    gpus: number;
	    gpuModel: string;
	    cpu: number;
	    memoryGB: number;
	
	    static createFrom(source: any = {}) {
	        return new ResourceInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.gpus = source["gpus"];
	        this.gpuModel = source["gpuModel"];
	        this.cpu = source["cpu"];
	        this.memoryGB = source["memoryGB"];
	    }
	}

}

