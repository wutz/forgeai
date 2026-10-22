// Injected before the app loads: stands in for the Wails Go bindings with a believable project.
(() => {
  const now = Date.now()
  const ago = (min) => new Date(now - min * 60000).toISOString()
  const age = (min) => min < 60 ? `${min}m` : min < 1440 ? `${Math.floor(min / 60)}h` : `${Math.floor(min / 1440)}d`
  const dur = (min) => `${Math.floor(min / 60)}h${String(min % 60).padStart(2, '0')}m`

  const worker = (i, phase, node) => ({index: i, podName: `pod-${i}`, nodeName: node, phase})
  const job = (o) => Object.assign({
    group: o.name, image: 'harbor.example.com/vision-lab/trainer:2.4', command: 'torchrun train.py --config configs/base.yaml',
    env: {}, instanceType: 'h100x8', workers: 1, stopped: false, superseded: false, ready: 0, gpuClass: 'h100', gpuCount: 8,
    workerPods: [], volumes: ['datasets', 'checkpoints'], maxRetries: 3, retryOn: 'Infrastructure', retries: [],
  }, o, {created: ago(o.min), age: age(o.min)})

  const jobs = [
    job({name: 'llama-sft-r2', phase: 'Running', workers: 4, ready: 4, gpuCount: 8, min: 312, duration: dur(312), group: 'llama-sft', retryOf: 'llama-sft',
      workerPods: [0, 1, 2, 3].map(i => worker(i, 'Running', `gpu-node-0${i + 1}`))}),
    job({name: 'vit-pretrain', phase: 'Running', workers: 2, ready: 2, gpuCount: 8, min: 1220, duration: dur(1220),
      workerPods: [0, 1].map(i => worker(i, 'Running', `gpu-node-0${i + 5}`))}),
    job({name: 'diffusion-ablate-3', phase: 'Queued', workers: 2, ready: 0, instanceType: 'rtx5090x8', gpuClass: 'rtx-5090', min: 18,
      message: 'Waiting for GPU quota in ClusterQueue forge-vision'}),
    job({name: 'eval-sweep-0412', phase: 'Succeeded', workers: 1, ready: 1, instanceType: 'rtx5090x4', gpuClass: 'rtx-5090', gpuCount: 4, min: 2400, duration: dur(137)}),
    job({name: 'llama-sft', phase: 'Failed', workers: 4, ready: 0, min: 640, duration: dur(94), failureReason: 'NodeFailure',
      message: 'Node gpu-node-03 became NotReady', retriedAs: 'llama-sft-r2'}),
    job({name: 'tokenizer-bench', phase: 'Succeeded', workers: 1, ready: 1, instanceType: 'rtx5090x1', gpuClass: 'rtx-5090', gpuCount: 1, min: 4300, duration: dur(21)}),
    job({name: 'sft-ablation-lr', phase: 'Succeeded', workers: 2, ready: 2, instanceType: 'rtx5090x8', gpuClass: 'rtx-5090', min: 3100, duration: dur(188)}),
    job({name: 'embed-finetune', phase: 'Succeeded', workers: 1, ready: 1, instanceType: 'rtx5090x4', gpuClass: 'rtx-5090', gpuCount: 4, min: 3900, duration: dur(75)}),
    job({name: 'reward-model-v3', phase: 'Succeeded', workers: 2, ready: 2, min: 6200, duration: dur(402)}),
    job({name: 'lora-merge-sweep', phase: 'Succeeded', workers: 1, ready: 1, instanceType: 'rtx5090x4', gpuClass: 'rtx-5090', gpuCount: 4, min: 7300, duration: dur(44)}),
    job({name: 'rl-rollout-7', phase: 'Stopped', workers: 2, ready: 0, instanceType: 'rtx5090x8', gpuClass: 'rtx-5090', min: 5800, stopped: true}),
  ]

  const sbx = (o) => Object.assign({
    image: 'harbor.example.com/vision-lab/agent:latest', env: {}, runtimeClass: 'gvisor', service: true, stopped: false,
    command: 'bash', volumes: ['workspace'], cpu: '8', memory: '32Gi', gpu: 0,
  }, o, {created: ago(o.min), age: age(o.min)})
  const sandboxes = [
    sbx({name: 'agent-dev', phase: 'Running', instanceType: 'rtx5090x1', gpu: 1, min: 95, nodeName: 'gpu-node-07', podName: 'agent-dev', podPhase: 'Running', serviceFQDN: 'agent-dev.forge-vision.svc', podIPs: ['10.244.7.21']}),
    sbx({name: 'notebook-alice', phase: 'Running', instanceType: 'rtx5090x1', gpu: 1, min: 410, nodeName: 'gpu-node-08', podName: 'notebook-alice', podPhase: 'Running', serviceFQDN: 'notebook-alice.forge-vision.svc', podIPs: ['10.244.8.12'], runtimeClass: 'kata'}),
    sbx({name: 'browser-agent', phase: 'Idle', min: 1500, idleSince: ago(42), podPhase: 'Running', nodeName: 'cpu-node-02'}),
    sbx({name: 'code-review-bot', phase: 'Suspended', min: 3200, stopped: true}),
    sbx({name: 'jupyter-bob', phase: 'Running', instanceType: 'rtx5090x1', gpu: 1, min: 210, nodeName: 'gpu-node-09', podName: 'jupyter-bob', podPhase: 'Running', serviceFQDN: 'jupyter-bob.forge-vision.svc', podIPs: ['10.244.9.4']}),
    sbx({name: 'eval-harness', phase: 'Suspended', min: 4100, stopped: true}),
    sbx({name: 'mcp-gateway', phase: 'Running', cpu: '4', memory: '8Gi', min: 8800, nodeName: 'cpu-node-02', podPhase: 'Running', podName: 'mcp-gateway', serviceFQDN: 'mcp-gateway.forge-vision.svc', podIPs: ['10.244.2.9']}),
    sbx({name: 'scraper-agent', phase: 'Idle', min: 2600, idleSince: ago(95), podPhase: 'Running', nodeName: 'cpu-node-01'}),
    sbx({name: 'data-prep', phase: 'Running', cpu: '16', memory: '64Gi', min: 55, nodeName: 'cpu-node-01', podPhase: 'Running', podName: 'data-prep', shutdownTime: new Date(now + 5 * 3600000).toISOString()}),
  ]

  const gib = (n) => n
  const vol = (name, type, req, cap, users, min, extra) => Object.assign({
    name, type, storageClass: type === 'fast' ? 'cephfs-ssd' : 'cephfs', phase: 'Bound', requestedGiB: req, capacityGiB: cap,
    accessMode: 'ReadWriteMany', created: ago(min), users, inTrash: false,
  }, extra)
  const vols = [
    vol('datasets', 'fast', 1536, 1536, ['llama-sft-r2', 'vit-pretrain'], 60 * 24 * 40),
    vol('checkpoints', 'fast', 1024, 1024, ['llama-sft-r2', 'vit-pretrain'], 60 * 24 * 38),
    vol('workspace', 'standard', 200, 200, ['agent-dev', 'notebook-alice'], 60 * 24 * 21),
    vol('models', 'standard', 512, 512, [], 60 * 24 * 30),
    vol('eval-cache', 'standard', 100, 100, ['eval-sweep-0412'], 60 * 24 * 9),
    vol('tokenized', 'fast', 64, 64, ['vit-pretrain'], 60 * 24 * 6),
    vol('artifacts', 'standard', 128, 128, [], 60 * 24 * 5),
    vol('logs', 'standard', 50, 50, ['llama-sft-r2'], 60 * 24 * 3),
  ]

  const types = [
    {name: 'h100x8', class: 'h100', product: 'NVIDIA H100 80GB', gpuCount: 8, gpuMemoryMiB: 81920, cpu: '96', memory: '1000Gi', total: 6, free: 2},
    {name: 'rtx5090x8', class: 'rtx-5090', product: 'NVIDIA RTX 5090', gpuCount: 8, gpuMemoryMiB: 32768, cpu: '64', memory: '512Gi', total: 6, free: 3},
    {name: 'rtx5090x4', class: 'rtx-5090', product: 'NVIDIA RTX 5090', gpuCount: 4, gpuMemoryMiB: 32768, cpu: '32', memory: '256Gi', total: 12, free: 6},
    {name: 'rtx5090x1', class: 'rtx-5090', product: 'NVIDIA RTX 5090', gpuCount: 1, gpuMemoryMiB: 32768, cpu: '8', memory: '64Gi', total: 48, free: 30},
  ]

  const img = (repo, tag, size, min, pulled) => ({repository: repo, digest: 'sha256:' + Math.random().toString(16).slice(2).padEnd(64, 'a').slice(0, 64), tags: [tag], sizeMiB: size, pushedAt: ago(min), pulledAt: pulled ? ago(pulled) : undefined})
  const images = [
    {name: 'vision-lab/trainer', count: 6, pulls: 214, images: [img('vision-lab/trainer', '2.4', 9120, 60 * 30, 12), img('vision-lab/trainer', '2.3', 8940, 60 * 24 * 12, 900), img('vision-lab/trainer', '2.2', 8810, 60 * 24 * 30, 4000)]},
    {name: 'vision-lab/agent', count: 4, pulls: 96, images: [img('vision-lab/agent', 'latest', 3410, 60 * 5, 30), img('vision-lab/agent', 'v0.8', 3390, 60 * 24 * 7, 1500)]},
    {name: 'vision-lab/notebook', count: 3, pulls: 41, images: [img('vision-lab/notebook', 'cuda12.4', 6720, 60 * 24 * 3, 410)]},
  ]

  const m = {
    Contexts: () => [{name: 'gpu-prod', cluster: 'gpu-prod', namespace: 'forge-vision', current: true}],
    Projects: () => [{namespace: 'forge-vision', project: 'vision-lab'}],
    Connect: () => ({context: 'gpu-prod', server: 'https://k8s.example.com:6443', namespace: 'forge-vision', project: 'vision-lab', admin: true, sandboxes: true}),
    Config: () => ({apiServer: '', ingress: {}, registry: {enabled: true, url: 'harbor.example.com'}, storageTypes: {fast: {storageClass: 'cephfs-ssd', accessMode: 'ReadWriteMany', description: 'NVMe-backed'}, standard: {storageClass: 'cephfs', accessMode: 'ReadWriteMany', description: 'Shared'}}, fileServer: {idleTimeout: {Duration: 900e9}}, trash: {Duration: 7 * 86400e9}, training: {defaultRuntime: 'torch-distributed', shmSize: '16Gi', stuckTimeout: {Duration: 600e9}, maxRetries: 3}}),
    ListJobs: () => jobs,
    GetJob: (n) => jobs.find(j => j.name === n),
    Logs: () => '',
    ListSandboxes: () => sandboxes,
    GetSandbox: (n) => sandboxes.find(s => s.name === n),
    SandboxOptions: () => ({defaultImage: 'harbor.example.com/vision-lab/agent:latest', images: [], runtimeClasses: ['gvisor', 'kata'], defaultRuntimeClass: 'gvisor', idleTimeout: '30m'}),
    SandboxTemplates: () => [],
    ListTemplates: () => [],
    TemplateNames: () => [],
    ListVolumes: (trash) => trash ? [
      Object.assign(vol('old-features', 'standard', 300, 300, [], 60 * 24 * 60), {inTrash: true, trashedAt: ago(60 * 24 * 2), expireAt: new Date(now + 5 * 86400000).toISOString()}),
      Object.assign(vol('scratch', 'fast', 500, 500, [], 60 * 24 * 40), {inTrash: true, trashedAt: ago(60 * 24 * 6), expireAt: new Date(now + 1 * 86400000).toISOString()}),
    ] : vols,
    Files: () => ({phase: 'Running', address: 'https://files.example.com', volumes: ['datasets', 'checkpoints', 'workspace']}),
    FileServerPhase: () => 'Running',
    ListImages: () => images,
    Registry: () => ({enabled: true, url: 'harbor.example.com', username: 'robot$vision-lab', secret: 'harbor-pull'}),
    ListInstanceTypes: () => types,
    Quota: () => ({
      gpuClasses: [{class: 'h100', quota: 16, used: 16, pods: 6}, {class: 'rtx-5090', quota: 24, used: 11, pods: 3}],
      storage: '4Ti', storageUsed: '3.5Ti', volumes: 8, volumeMax: 10, pods: 14, podMax: 100,
    }),
    ListProjects: () => [],
    Heartbeat: () => {},
  }

  const noop = () => {}
  window.go = {app: {App: new Proxy({}, {get: (_, name) => (...args) => Promise.resolve(name in m ? JSON.parse(JSON.stringify(m[name](...args) ?? null)) : null)})}}
  window.runtime = new Proxy({}, {get: (_, name) => name === 'Environment' ? () => Promise.resolve({platform: window.__platform || 'linux', buildType: 'production', arch: 'arm64'}) : noop})
})()
