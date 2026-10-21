// All visible copy lives here. `zh` is typed against `en`, so a key missing from one language fails `astro build`.

export const locales = ['en', 'zh'] as const;
export type Locale = (typeof locales)[number];

// URL prefix of each locale. English is the root so the default URLs stay short.
export const localeBase: Record<Locale, string> = { en: '/', zh: '/zh/' };

export const htmlLang: Record<Locale, string> = { en: 'en', zh: 'zh-CN' };

const en = {
  siteName: 'forgeai',
  homeTitle: 'forgeai: Kubernetes-native AI platform',
  homeDescription:
    'forgeai is a Kubernetes-native AI platform. Multi-tenant projects with quotas and isolation, distributed training with Kubeflow Trainer and Kueue, agent sandboxes and shared volumes, all as plain Kubernetes objects.',
  notFoundTitle: 'Page not found · forgeai',
  notFoundDescription: 'This page does not exist.',

  header: {
    home: 'forgeai home',
    mainNav: 'Main',
    features: 'Features',
    how: 'How it works',
    start: 'Get started',
    github: 'GitHub',
    language: 'Language',
    theme: 'Theme',
    themeSystem: 'System',
    themeLight: 'Light',
    themeDark: 'Dark',
    switchTo: '中文',
  },

  footer: {
    copyright: '© forgeai',
    source: 'Source on GitHub',
    docs: 'Documentation',
  },

  hero: {
    eyebrow: 'First milestone · Multi-tenant training',
    title: 'The AI platform for teams on Kubernetes',
    lead: 'forgeai gives every team a project on a shared GPU cluster, with quotas, network isolation and member access. Training jobs, sandboxes and volumes are ordinary Kubernetes objects, so they work with the tools you already run.',
    primary: 'Get started',
    secondary: 'View on GitHub',
    builtOn: 'Built on',
    projectCaption: 'Example Project resource',
  },

  features: {
    eyebrow: 'Features',
    title: 'Everything a shared AI cluster needs, in one place',
    lead: 'forgeai is a Kubernetes operator with a desktop console. It adds the parts that Trainer, Kueue and the sandbox controller leave out: tenancy, lifecycle, retries and access.',
    items: [
      {
        icon: 'PRJ',
        title: 'Multi-tenant projects',
        body: 'A Project is a tenant: its own namespace, GPU quota per class, storage and volume limits, network isolation, a registry robot and members with edit or view access.',
      },
      {
        icon: 'TRN',
        title: 'Distributed training',
        body: 'Jobs are plain Kubeflow Trainer v2 TrainJobs, admitted by Kueue. Jobs over quota wait in the queue instead of failing.',
      },
      {
        icon: 'RTY',
        title: 'Failure handling',
        body: 'Stuck jobs are detected and stopped. Failures are classified (node failure, eviction, OOM, image pull, config, exit code) and retried when the policy allows.',
      },
      {
        icon: 'SBX',
        title: 'Agent sandboxes',
        body: 'Interactive workloads run as agent-sandbox Sandboxes under gVisor or Kata. Idle sandboxes suspend, and a TTL stops them on schedule.',
      },
      {
        icon: 'VOL',
        title: 'Volumes and files',
        body: 'Shared ReadWriteMany volumes resize, go to a trash with an expiry, and restore. An on-demand file server browses them and scales to zero when idle.',
      },
      {
        icon: 'GPU',
        title: 'Instance types',
        body: 'GPU classes and sizes are generated from the nodes of the cluster, with live capacity per node. Members pick an instance type; they never edit the catalogue.',
      },
    ],
  },

  how: {
    eyebrow: 'How it works',
    title: 'No API server of its own',
    lead: "The operator reconciles Kubernetes objects. The desktop console and kubectl create those objects with each member's own credentials, so Kubernetes RBAC decides what everyone can do.",
    steps: [
      {
        title: 'An admin creates a Project',
        body: 'Quota, network isolation, trash retention and the list of members go into one cluster-scoped resource.',
      },
      {
        title: 'The operator provisions the tenant',
        body: 'It creates the namespace, queues, quotas, network policy, registry project and member credentials.',
      },
      {
        title: 'Members work inside their project',
        body: 'They submit jobs, start sandboxes and manage volumes with their own kubeconfig. They cannot change quotas, policies or access.',
      },
    ],
    tableObject: 'Object',
    tableName: 'Name',
    tablePurpose: 'Purpose',
    objects: [
      ['Namespace', 'forge-<project>', 'Pod security baseline, network policies and labels that tie it to the project.'],
      ['ClusterQueue + LocalQueue', 'forge-<project> / default', 'GPU quota per class. Jobs over quota wait in the queue.'],
      ['ResourceQuota', 'forgeai-quota', 'Backstop for workloads that bypass Kueue: GPUs, CPU, memory, storage, volumes and pods.'],
      ['NetworkPolicy', 'forgeai-isolation', 'Ingress only from the namespace and platform namespaces; optional internet egress block.'],
      ['Registry project + robot', 'forge-<project>', 'Harbor project with a pull secret and push credentials, plus a storage quota.'],
      ['Member ServiceAccount', 'forgeai-members/<project>.<member>', 'A token bound to the project edit or view role. Removing the member revokes it.'],
    ] as [string, string, string][],
  },

  training: {
    eyebrow: 'Training',
    title: 'Jobs that recover on their own',
    lead: 'Training runs as Kubeflow Trainer v2 TrainJobs in the project namespace, queued by Kueue. The operator adds the lifecycle features Trainer does not provide, so a long run does not need someone watching it.',
    bullets: [
      '<strong>Stop and start</strong> release the pods and keep the job, then reactivate its Kueue workload.',
      '<strong>Rerun</strong> submits a new job from the form of an existing one.',
      '<strong>Stuck detection</strong> stops jobs whose pods cannot start, such as image pull errors or crash loops.',
      '<strong>Retries</strong> resubmit failed jobs as <code>&lt;group&gt;-r&lt;n&gt;</code> up to a limit. Infrastructure failures retry by default.',
    ],
    tableReason: 'Failure reason',
    tableRetried: 'Retried by default',
    yes: 'Yes',
    never: 'Never',
    onlyAny: 'Only with <code>retry-on: Any</code>',
  },

  sandboxes: {
    eyebrow: 'Sandboxes and volumes',
    title: 'Interactive work without idle GPUs',
    bullets: [
      '<strong>Sandboxes</strong> are agent-sandbox resources with gVisor or Kata isolation, a bash command, env, optional GPU placement and project volumes.',
      '<strong>Idle suspend</strong> stops a sandbox nothing has used for its timeout. Open clients send a heartbeat to keep it awake.',
      '<strong>Volumes</strong> are PVCs from the platform storage types. They can grow and go to a trash with an expiry before they are purged.',
      '<strong>File server</strong> serves each volume over HTTPS with basic auth and scales to zero when it is not in use.',
    ],
    caption: 'Example Sandbox resource',
  },

  start: {
    eyebrow: 'Get started',
    title: 'Install the operator and the console',
    lead: 'forgeai needs a Kubernetes cluster with Kubeflow Trainer v2 and Kueue installed. Sandboxes need agent-sandbox, and project registries need Harbor. The steps below run from a checkout of the repository.',
    steps: [
      {
        title: '1. Install the CRDs and the operator',
        body: 'Targets the cluster in your current kubeconfig context. Replace the image with one your cluster can pull.',
      },
      {
        title: '2. Create a project',
        body: 'The repository ships a sample Project you can adapt.',
      },
      {
        title: '3. Build the desktop console',
        body: 'The console is a Wails app that talks to the cluster through a kubeconfig context.',
      },
    ],
    note: {
      lead: 'Full reference: the',
      readme: 'README',
      middle: 'covers the object model, and',
      gui: 'gui/',
      tail: 'covers the console.',
    },
  },

  notFound: {
    eyebrow: '404',
    title: 'Page not found',
    body: 'The page you were looking for does not exist. Head back to the',
    home: 'forgeai home page',
    or: 'or read the',
    docs: 'documentation',
    end: '.',
  },
};

export type Messages = typeof en;

const zh: Messages = {
  siteName: 'forgeai',
  homeTitle: 'forgeai：Kubernetes 原生 AI 平台',
  homeDescription:
    'forgeai 是 Kubernetes 原生的 AI 平台。提供带配额与隔离的多租户项目、基于 Kubeflow Trainer 与 Kueue 的分布式训练、智能体沙箱和共享存储卷，全部以标准 Kubernetes 对象的形式提供。',
  notFoundTitle: '页面不存在 · forgeai',
  notFoundDescription: '该页面不存在。',

  header: {
    home: 'forgeai 首页',
    mainNav: '主导航',
    features: '功能',
    how: '工作原理',
    start: '快速开始',
    github: 'GitHub',
    language: '语言',
    theme: '主题',
    themeSystem: '跟随系统',
    themeLight: '浅色',
    themeDark: '深色',
    switchTo: 'English',
  },

  footer: {
    copyright: '© forgeai',
    source: 'GitHub 源码',
    docs: '文档',
  },

  hero: {
    eyebrow: '第一个里程碑 · 多租户训练',
    title: '面向团队的 Kubernetes AI 平台',
    lead: 'forgeai 为每个团队在共享 GPU 集群上提供一个项目，包含配额、网络隔离和成员权限。训练任务、沙箱和存储卷都是标准的 Kubernetes 对象，可以直接配合你现有的工具使用。',
    primary: '开始使用',
    secondary: '在 GitHub 查看',
    builtOn: '基于',
    projectCaption: 'Project 资源示例',
  },

  features: {
    eyebrow: '功能',
    title: '共享 AI 集群所需的一切，集中在一处',
    lead: 'forgeai 是一个 Kubernetes Operator，并配有桌面控制台。它补充了 Trainer、Kueue 和沙箱控制器没有提供的部分：多租户、生命周期、重试与访问控制。',
    items: [
      {
        icon: 'PRJ',
        title: '多租户项目',
        body: '项目就是一个租户：拥有独立的命名空间、按 GPU 类型划分的配额、存储与卷的上限、网络隔离、镜像仓库机器人账号，以及具有编辑或只读权限的成员。',
      },
      {
        icon: 'TRN',
        title: '分布式训练',
        body: '任务是标准的 Kubeflow Trainer v2 TrainJob，由 Kueue 调度准入。超出配额的任务会在队列中等待，而不是直接失败。',
      },
      {
        icon: 'RTY',
        title: '故障处理',
        body: '卡住的任务会被检测并停止。失败会被分类（节点故障、驱逐、OOM、镜像拉取、配置、退出码），并在策略允许时自动重试。',
      },
      {
        icon: 'SBX',
        title: '智能体沙箱',
        body: '交互式负载以 agent-sandbox Sandbox 的形式运行，可选 gVisor 或 Kata 隔离。空闲的沙箱会自动挂起，并可通过 TTL 按时停止。',
      },
      {
        icon: 'VOL',
        title: '存储卷与文件',
        body: '共享的 ReadWriteMany 存储卷可以扩容、移入回收站并设置过期时间，也可以恢复。按需启动的文件服务器用于浏览这些卷，空闲时缩容至零。',
      },
      {
        icon: 'GPU',
        title: '实例类型',
        body: '根据集群节点自动生成 GPU 类型与规格，并实时显示每个节点的可用容量。成员选择实例类型，但无法修改目录本身。',
      },
    ],
  },

  how: {
    eyebrow: '工作原理',
    title: '没有自己的 API 服务器',
    lead: 'Operator 负责协调 Kubernetes 对象。桌面控制台和 kubectl 使用每位成员自己的凭据创建这些对象，因此由 Kubernetes RBAC 决定每个人能做什么。',
    steps: [
      {
        title: '管理员创建项目',
        body: '配额、网络隔离、回收站保留时间和成员列表都写在一个集群级资源中。',
      },
      {
        title: 'Operator 为租户完成配置',
        body: '创建命名空间、队列、配额、网络策略、镜像仓库项目以及成员凭据。',
      },
      {
        title: '成员在自己的项目内工作',
        body: '使用各自的 kubeconfig 提交任务、启动沙箱并管理存储卷。成员无法修改配额、策略或访问权限。',
      },
    ],
    tableObject: '对象',
    tableName: '名称',
    tablePurpose: '用途',
    objects: [
      ['Namespace', 'forge-<project>', '带有 baseline 级别 Pod 安全策略、网络策略以及将其与项目关联的标签。'],
      ['ClusterQueue + LocalQueue', 'forge-<project> / default', '按 GPU 类型设置配额。超出配额的任务在队列中等待。'],
      ['ResourceQuota', 'forgeai-quota', '兜底限制，针对绕过 Kueue 的负载：GPU、CPU、内存、存储、卷和 Pod 数量。'],
      ['NetworkPolicy', 'forgeai-isolation', '仅允许来自命名空间和平台命名空间的入站流量；可选择阻断出站的互联网访问。'],
      ['Registry project + robot', 'forge-<project>', 'Harbor 项目，附带拉取 Secret 和推送凭据，并设置存储配额。'],
      ['Member ServiceAccount', 'forgeai-members/<project>.<member>', '绑定到项目 edit 或 view 角色的令牌。移除成员即撤销其令牌。'],
    ] as [string, string, string][],
  },

  training: {
    eyebrow: '训练',
    title: '任务可以自行恢复',
    lead: '训练以 Kubeflow Trainer v2 TrainJob 的形式运行在项目命名空间中，由 Kueue 排队调度。Operator 补充了 Trainer 不提供的生命周期功能，因此长时间运行的任务无需有人一直盯着。',
    bullets: [
      '<strong>启停</strong>会释放 Pod 并保留任务，之后可以重新激活其 Kueue Workload。',
      '<strong>重新运行</strong>可以基于已有任务的表单提交一个新任务。',
      '<strong>卡住检测</strong>会停止无法启动 Pod 的任务，例如镜像拉取错误或崩溃循环。',
      '<strong>重试</strong>会以 <code>&lt;group&gt;-r&lt;n&gt;</code> 的名称重新提交失败的任务，直到达到上限。默认重试基础设施故障。',
    ],
    tableReason: '失败原因',
    tableRetried: '默认是否重试',
    yes: '是',
    never: '从不',
    onlyAny: '仅在 <code>retry-on: Any</code> 时',
  },

  sandboxes: {
    eyebrow: '沙箱与存储卷',
    title: '交互式工作，不让 GPU 空转',
    bullets: [
      '<strong>沙箱</strong>是 agent-sandbox 资源，支持 gVisor 或 Kata 隔离，可配置 bash 命令、环境变量、可选的 GPU 调度以及项目存储卷。',
      '<strong>空闲挂起</strong>会停止超过超时时间未被使用的沙箱。打开的客户端会发送心跳来保持其唤醒。',
      '<strong>存储卷</strong>来自平台的存储类型，可以扩容；删除时先进入回收站并设置过期时间，之后再清除。',
      '<strong>文件服务器</strong>通过 HTTPS 提供每个存储卷的访问，并使用基础认证；不使用时缩容至零。',
    ],
    caption: 'Sandbox 资源示例',
  },

  start: {
    eyebrow: '快速开始',
    title: '安装 Operator 与控制台',
    lead: 'forgeai 需要已安装 Kubeflow Trainer v2 和 Kueue 的 Kubernetes 集群。沙箱需要 agent-sandbox，项目镜像仓库需要 Harbor。以下步骤在仓库的本地检出目录中执行。',
    steps: [
      {
        title: '1. 安装 CRD 与 Operator',
        body: '针对当前 kubeconfig 上下文中的集群执行。请将镜像替换为集群可以拉取的镜像。',
      },
      {
        title: '2. 创建项目',
        body: '仓库中附带一个示例 Project，可以在此基础上修改。',
      },
      {
        title: '3. 构建桌面控制台',
        body: '控制台是一个 Wails 应用，通过 kubeconfig 上下文与集群通信。',
      },
    ],
    note: {
      lead: '完整参考：',
      readme: 'README',
      middle: '介绍对象模型，另见',
      gui: 'gui/',
      tail: '介绍控制台。',
    },
  },

  notFound: {
    eyebrow: '404',
    title: '页面不存在',
    body: '您访问的页面不存在。请返回',
    home: 'forgeai 首页',
    or: '或阅读',
    docs: '文档',
    end: '。',
  },
};

export const messages: Record<Locale, Messages> = { en, zh };
