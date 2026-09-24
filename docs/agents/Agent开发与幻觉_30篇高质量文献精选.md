**Agent 开发与幻觉：30 篇高质量文献与文章精选**

从学术论文引用量、社区阅读量与内容质量三个维度综合筛选

本清单聚焦"Agent 开发"与"大模型幻觉"的交叉领域，共收录 30 篇高质量文献与文章，分为五个板块：幻觉综述与奠基、幻觉检测与评估基准、Agent 开发奠基之作、Agent×幻觉交叉专题、高阅读量博客与工程实践。论文引用量为 Google Scholar 检索约数（截至 2026 年 9 月，会随时间增长）；筛选时交叉主题文献优先，纯多模态幻觉、纯法律/医疗垂直领域的文献未纳入。

**目录**

*在 Word 中右键目录并选择"更新域"可刷新页码。*

一、幻觉问题综述与奠基（理解全貌必读） 2

二、幻觉检测与评估基准 4

三、Agent 开发奠基之作 6

四、Agent × 幻觉交叉专题（与主题最直接相关） 9

五、高阅读量博客与工程实践（业界影响力最大） 12

**一、幻觉问题综述与奠基（理解全貌必读）**

**1. Survey of Hallucination in Natural Language Generation**

Ji et al. · ACM Computing Surveys, 2023 · 引用约 11,600

幻觉研究领域引用量最高的综述，系统定义了幻觉的分类、成因、评估指标与缓解方法，是入门第一文献。

<https://dl.acm.org/doi/abs/10.1145/3571730>

**2. A Survey on Hallucination in Large Language Models: Principles, Taxonomy, Challenges, and Open Questions**

Huang et al. · ACM TOIS, 2025 · 引用约 7,400

LLM 时代幻觉综述的标杆，提出了被广泛沿用的事实性/忠实性幻觉分类框架。

<https://dl.acm.org/doi/abs/10.1145/3703155>

**3. A Comprehensive Survey of Hallucination Mitigation Techniques in Large Language Models**

Tonmoy et al. · arXiv, 2024 · 引用约 1,000

逐一梳理 32 种幻觉缓解技术（RAG、微调、解码策略、提示工程等），工程参考价值极高。

<https://arxiv.org/abs/2401.01313>

**4. A Survey of Hallucination in Large Foundation Models**

Rawte et al. · arXiv, 2023 · 引用约 860

覆盖文本、图像、视频等多模态基础模型的幻觉问题全景。

<https://arxiv.org/abs/2309.05922>

**5. Hallucination is Inevitable: An Innate Limitation of Large Language Models**

Xu et al. · arXiv, 2024 · 引用约 1,200

从理论层面证明幻觉是 LLM 的固有局限、无法彻底消除。做 Agent 可靠性设计前必读，它决定了你必须用工程手段兜底。

<https://arxiv.org/abs/2401.11817>

**二、幻觉检测与评估基准**

**6. TruthfulQA: Measuring How Models Mimic Human Falsehoods**

Lin et al. · ACL 2022 · 引用约 4,700

最经典的真实性基准，揭示了模型越大越容易模仿人类谬误的反直觉现象。

<https://aclanthology.org/2022.acl-long.229/>

**7. SelfCheckGPT: Zero-Resource Black-Box Hallucination Detection for Generative LLMs**

Manakul et al. · EMNLP 2023 · 引用约 2,400

幻觉检测引用量最高的方法论文：多次采样比对一致性即可黑盒检测幻觉，Agent 系统里至今常用。

<https://aclanthology.org/2023.emnlp-main.557/>

**8. HaluEval: A Large-Scale Hallucination Evaluation Benchmark for LLMs**

Li et al. · EMNLP 2023 · 引用约 1,200

首个大规模幻觉评估基准，覆盖问答、对话、摘要三大场景。

<https://aclanthology.org/2023.emnlp-main.397/>

**9. Chain-of-Verification Reduces Hallucination in Large Language Models**

Dhuliawala et al. · ACL 2024 Findings · 引用约 1,300

"自我验证链"方法，是 Agent 自我纠错类设计的思想源头之一。

<https://aclanthology.org/2024.findings-acl.212/>

**10. Fine-Grained Hallucination Detection and Editing for Language Models（FAVA）**

Mishra et al. · arXiv, 2024 · 引用约 220

把幻觉检测做到 span 级细粒度，并给出编辑修正方案。

<https://arxiv.org/abs/2401.06855>

**11. The Dawn After the Dark: An Empirical Study on Factuality Hallucination in the ChatGPT Era**

Li et al. · ACL 2024 · 引用约 360

对主流模型幻觉率的大规模实证测量，选型参考。

<https://aclanthology.org/2024.acl-long.586/>

**12. ReDeep: Detecting Hallucination in Retrieval-Augmented Generation via Mechanism Interpretability**

Sun et al. · ICLR 2025 · 引用约 180

用可解释性方法检测 RAG 幻觉，代表"从模型内部信号识别幻觉"的前沿路线。

<https://proceedings.iclr.cc/paper_files/paper/2025/hash/7daf60e805e596cb3ecbf804be8f95e3-Abstract-Conference.html>

**三、Agent 开发奠基之作**

**13. ReAct: Synergizing Reasoning and Acting in Language Models**

Yao et al. · ICLR 2023 · 引用约 16,000

推理+行动交替范式的开山之作，几乎所有现代 Agent 框架的基础。

<https://arxiv.org/abs/2210.03629>

**14. Toolformer: Language Models Can Teach Themselves to Use Tools**

Schick et al. · NeurIPS 2023 · 引用约 7,300

工具学习奠基论文，Agent 调用外部工具（也是抗幻觉手段）的起点。

<https://proceedings.neurips.cc/paper_files/paper/2023/hash/d842425e4bf79ba039352da0f658a906-Abstract-Conference.html>

**15. Reflexion: Language Agents with Verbal Reinforcement Learning**

Shinn et al. · NeurIPS 2023 · 引用约 7,800

Agent 自我反思与纠错机制的开创性工作，直接回应"Agent 如何从错误/幻觉中恢复"。

<https://proceedings.neurips.cc/paper_files/paper/2023/hash/1b44b878bb782e6954cd888628510e90-Abstract-Conference.html>

**16. Retrieval-Augmented Generation for Knowledge-Intensive NLP Tasks**

Lewis et al. · NeurIPS 2020 · 引用约 30,000

RAG 原始论文——对抗幻觉最重要的架构思想，Agent 开发者的必修课。

<https://proceedings.neurips.cc/paper_files/paper/2020/hash/6b493230205f780e1bc26945df7481e5-Abstract.html>

**17. A Survey on Large Language Model based Autonomous Agents**

Wang et al. · Frontiers of Computer Science, 2024 · 引用约 5,300

Agent 综述中引用最高者，"Profile—Memory—Planning—Action"四模块框架被广泛引用。

<https://link.springer.com/article/10.1007/s11704-024-40231-1>

**18. The Rise and Potential of Large Language Model Based Agents: A Survey**

Xi et al. · 2023/2025 · 引用约 3,400

另一篇顶级 Agent 综述，对"大脑—感知—行动"的拆解非常清晰。

<https://link.springer.com/article/10.1007/s11432-024-4222-0>

**19. ToolLLM: Facilitating Large Language Models to Master 16000+ Real-world APIs**

Qin et al. · ICLR 2024 · 引用约 2,500

大规模真实 API 工具使用基准，暴露了大量工具调用幻觉问题。

<https://proceedings.iclr.cc/paper_files/paper/2024/hash/28e50ee5b72e90b6ab7e0f9ebc68f0c8-Abstract-Conference.html>

**四、Agent × 幻觉交叉专题（与主题最直接相关）**

**20. LLM-based Agents Suffer from Hallucinations: A Survey of Taxonomy, Methods, and Directions**

Lin et al. · arXiv, 2025 · 引用约 80

目前唯一系统梳理 Agent 特有幻觉的综述：把幻觉分为规划、执行（工具选择/参数编造）、记忆、通信（多智能体）等类型，强烈推荐。

<https://arxiv.org/abs/2509.18970>

**21. Mitigating Hallucination in LLMs: An Application-Oriented Survey on RAG, Reasoning, and Agentic Systems**

Li et al. · arXiv, 2025 · 引用约 40

少有的从"应用落地"视角讲如何在 RAG、推理和 Agentic 系统中可控地降低幻觉。

<https://arxiv.org/abs/2510.24476>

**22. AgentHallu: Benchmarking Automated Hallucination Attribution of LLM-based Agents**

Liu et al. · arXiv, 2026

首个 Agent 幻觉归因基准：在多步轨迹中定位幻觉发生在哪一步。最强模型也仅 41.1% 的步骤定位准确率，工具幻觉最难检测。

<https://arxiv.org/abs/2601.06818>

**23. ToolBeHonest: A Multi-level Hallucination Diagnostic Benchmark for Tool-Augmented LLMs**

Zhang et al. · EMNLP 2024 · 引用约 70

工具增强场景的多层幻觉诊断基准，专门测"编造不存在的工具"这类问题。

<https://aclanthology.org/2024.emnlp-main.637/>

**24. Mirage-Bench: LLM Agent is Hallucinating and Where to Find Them**

Zhang et al. · arXiv, 2025

把 Agent 幻觉定义为"对观测不忠实的动作"，在多步交互环境中系统测量。

<https://arxiv.org/abs/2507.21017>

**25. The Reasoning Trap: How Enhancing LLM Reasoning Amplifies Tool Hallucination**

Yin et al. · ACL 2026

反直觉发现：强化推理能力反而会放大工具幻觉——对设计推理型 Agent 有直接警示价值。

<https://aclanthology.org/2026.acl-long.376/>

**五、高阅读量博客与工程实践（业界影响力最大）**

**26. Why Language Models Hallucinate（OpenAI 官方论文+博客）**

Kalai et al. · OpenAI, 2025-09 · 论文引用约 660

核心论点：幻觉源于训练与评测机制"奖励猜测、惩罚说不知道"，应通过改革基准评分（答错倒扣、承认不确定得分）来根治。发布后引发全网现象级讨论。

<https://openai.com/index/why-language-models-hallucinate/>

**27. LLM Powered Autonomous Agents（Lilian Weng）**

Lilian Weng · Lil'Log, 2023-06 · Agent 领域阅读量最高的博客

"Agent = LLM + 规划 + 记忆 + 工具使用"公式的出处，结尾专门讨论幻觉等挑战，Agent 入门必读。

<https://lilianweng.github.io/posts/2023-06-23-agent/>

**28. Extrinsic Hallucinations in LLMs（Lilian Weng）**

Lilian Weng · Lil'Log, 2024-07 · 24 篇参考文献

万字长文详解外在幻觉的成因、检测与对抗方法，中文社区由量子位全文翻译传播。

<https://lilianweng.github.io/posts/2024-07-07-hallucination/>

**29. Building Effective Agents（Anthropic）**

Erik Schluntz & Barry Zhang · Anthropic Engineering Blog, 2024-12

Agent 工程领域传播最广的文章："能用工作流就别上 Agent""保持简单、让 Agent 的上下文透明可控"——本质上都是防幻觉/防错误传播的工程原则。

<https://www.anthropic.com/engineering/building-effective-agents>

**30. Writing Effective Tools for AI Agents（Anthropic）**

Anthropic Engineering Blog, 2025-09

教你为 Agent 设计"防幻觉"的工具接口：清晰的工具描述、错误优雅处理、让工具自我解释——直接对应学术界所说的"工具文档缺陷导致执行幻觉"。

<https://www.anthropic.com/engineering/writing-tools-for-agents>

**六、延伸阅读（未计入 30 篇但值得一看）**

**Agents（Chip Huyen）**

Chip Huyen 博客，2025-01 · Agent 概念与评估的系统性科普

<https://huyenchip.com/2025/01/07/agents.html>

**Agents 白皮书（Google）**

Google, 2024 · 工业界视角的 Agent 架构入门

<https://www.kaggle.com/whitepaper-agents>

**Hallucination Cascade: Analyzing Error Propagation in Multi-Agent Systems**

Jamshidi et al., 2026 · 多智能体幻觉级联传播分析

<https://arxiv.org/abs/2606.07937>

**Vectara 幻觉排行榜**

持续更新的主流模型幻觉率实测榜单

<https://github.com/vectara/hallucination-leaderboard>
