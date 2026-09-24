# Agent 构建技术：30 篇高质量讨论精选

> 从学术论文引用量、社区阅读量与内容质量三个维度综合筛选。
> 论文引用量为 Google Scholar 检索约数（截至 2026 年 9 月，会随时间增长）。
> 整理日期：2026-09-23 · 与《幻觉》《上下文管理》《记忆》三份清单配套，构成 Agent 四部曲。

**导言**：Agent 构建技术回答的问题是"如何让 LLM 自主完成多步任务"。基石是 ReAct（推理+行动交替，ICLR 2023，引用约 16,000）与 Reflexion（自我反思，NeurIPS 2023，引用约 7,800）——这两篇已收录于《幻觉》清单，本清单不再重复，直接在其之上展开六大板块：**规划与推理范式 → 工具使用 → 多智能体 → 自我进化 → 评测基准 → 工程实践**。

---

## 一、规划与推理范式（Agent 的"思考"方式）

### 1. Tree of Thoughts: Deliberate Problem Solving with Large Language Models
- Yao et al. · NeurIPS 2023 · 引用约 8,780
- 把 CoT 的线性推理扩展为**树状搜索**：分支思考、自我评估、回溯剪枝。复杂规划类任务（24 点游戏、创意写作）成功率数倍提升，是"Agent 规划"的技术源头。
- 链接：<https://arxiv.org/abs/2305.10601>

### 2. Self-Refine: Iterative Refinement with Self-Feedback
- Madaan et al. · NeurIPS 2023 · 引用约 6,187
- "生成→自我批评→改写"循环，无需额外训练即可提升输出质量 20% 左右。是 Reflection 模式的奠基论文，也是 Andrew Ng 四大模式之首的学术出处。
- 链接：<https://arxiv.org/abs/2303.17651>

### 3. Graph of Thoughts: Solving Elaborate Problems with Large Language Models
- Besta et al. · AAAI 2024 · 引用约 2,537
- 进一步把推理结构推广为**任意图**：支持聚合（多个分支合成一个答案）、拆分、循环，比 ToT 更贴近真实复杂任务的依赖结构。
- 链接：<https://arxiv.org/abs/2308.09687>

### 4. LATS: Language Agent Tree Search Unifies Reasoning, Acting and Planning
- Zhou et al. · ICML 2024 · 引用约 746
- 把蒙特卡洛树搜索（MCTS）引入 Agent：用环境反馈做节点估值，统一了推理、行动与规划。是"搜索式 Agent"的代表框架。
- 链接：<https://arxiv.org/abs/2310.04406>

### 5. Plan-and-Solve Prompting: Improving Zero-Shot Chain-of-Thought Reasoning
- Wang et al. · ACL 2023
- "先制定计划、再分步执行"的零样本提示范式，是 Planner-Executor 架构的提示层原型，工程实现成本极低。
- 链接：<https://aclanthology.org/2023.acl-long.147/>

### 6. Self-Discover: Large Language Models Self-Compose Reasoning Structures
- Zhou et al.（Google DeepMind）· 2024
- 让模型自己**选择并组合**推理模块（分解、批判性思考等）形成任务专属的推理结构，比固定范式更灵活，比无结构更可靠。
- 链接：<https://arxiv.org/abs/2402.03620>

---

## 二、工具使用与函数调用（Agent 的"手"）

### 7. Gorilla: Large Language Model Connected with Massive APIs
- Patil et al.（UC Berkeley）· NeurIPS 2024 · 引用约 1,854
- 检索感知微调的 API 调用模型，在 1,600+ API 上调用准确率超越 GPT-4，并显著减少幻觉式调用。开启"工具调用专门化训练"路线。
- 链接：<https://arxiv.org/abs/2305.15334>

### 8. Berkeley Function Calling Leaderboard（BFCL）
- Yan et al. · 2025 · 引用约 736
- 函数调用能力的事实标准榜单：单轮/多轮/并行/多函数/相关性拒识全覆盖，工具调用选型和回归测试必备。
- 链接：<https://openreview.net/forum?id=2GmDdhBdDk>

### 9. API-Bank: A Comprehensive Benchmark for Tool-Augmented LLMs
- Li et al.（阿里）· EMNLP 2023
- 最早的工具增强基准之一：73 个 API、314 轮对话，把工具使用拆解为"感知需求→规划调用→执行"三级能力评估。
- 链接：<https://arxiv.org/abs/2304.08244>

---

## 三、多智能体系统（Agent 的"组织"方式）

### 10. AutoGen: Enabling Next-Gen LLM Applications via Multi-Agent Conversation
- Wu et al.（Microsoft）· 2023 · 引用约 5,185
- 微软的多智能体对话框架：把协作抽象为"可对话的 Agent 之间的消息传递"，支持人机回环、代码执行。多智能体框架中引用最高。
- 链接：<https://arxiv.org/abs/2308.08155>

### 11. MetaGPT: Meta Programming for a Multi-Agent Collaborative Framework
- Hong et al. · ICLR 2024 · 引用约 4,122
- 把**软件公司的 SOP**（产品经理→架构师→工程师→测试）编码进多智能体协作，用结构化文档而非自由对话传递信息——"角色+流程"式多智能体的代表作。
- 链接：<https://arxiv.org/abs/2308.00352>

### 12. CAMEL: Communicative Agents for "Mind" Exploration of Large Language Model Society
- Li et al. · NeurIPS 2023 · 引用约 2,825
- 角色扮演式双智能体协作（inception prompting）的开山之作，也是最早系统化研究"Agent 社会"的工作。
- 链接：<https://arxiv.org/abs/2303.17760>

### 13. ChatDev: Communicative Agents for Software Development
- Qian et al. · ACL 2024
- 虚拟软件公司：用瀑布模型组织多 Agent 开发软件，每个阶段双人结对（CEO/CTO/程序员/评审）。开源传播极广（GitHub 约 3 万星）。
- 链接：<https://arxiv.org/abs/2307.07924>

### 14. Multi-Agent Collaboration Mechanisms: A Survey of LLMs
- Tran et al. · 2025 · 引用约 791
- 多智能体协作机制的综述：按协作类型（合作/竞争/竞合）、消息结构、拓扑结构分类，含 AutoGen/MetaGPT/CAMEL 等框架对比表。
- 链接：<https://arxiv.org/abs/2501.06322>

---

## 四、自我进化与终身学习（Agent 的"成长"机制）

### 15. A Survey of Self-Evolving Agents: What, When, How, and Where to Evolve
- Fang et al. · 2025 · 引用约 286
- 自我进化 Agent 的系统综述：以"进化什么（模型/记忆/工具/架构）—何时进化（任务内/任务间）—如何进化（反馈/搜索/训练）—在哪进化"四问为框架。
- 链接：<https://arxiv.org/abs/2507.21046>

### 16. A Comprehensive Survey of Self-Evolving AI Agents: A New Paradigm Shifting from Model to Experience
- 2025 · 引用约 208
- 提出"从模型中心到经验中心"的范式转移论点：Agent 的长期竞争力在于经验（轨迹、记忆、技能）的积累与复用，而非单次模型能力。
- 链接：<https://arxiv.org/abs/2508.07407>

### 17. A Survey on Self-Evolution of Large Language Models
- Tao et al. · 2024 · 引用约 218
- 较早的 LLM 自进化综述：经验获取→经验精炼→更新→评估的迭代循环框架，覆盖 STaR、Self-Instruct 等经典路线。
- 链接：<https://arxiv.org/abs/2404.14387>

---

## 五、评测基准（构建得好不好，拿什么测）

### 18. SWE-bench: Can Language Models Resolve Real-World GitHub Issues?
- Jimenez et al.（Princeton）· ICLR 2024 · 引用约 4,668
- 软件工程 Agent 的黄金基准：真实 GitHub issue + 仓库级代码修改 + 单测验证。催生了整个 Coding Agent 赛道（解决率从 2024 年初约 2% 攀升至如今 70%+）。
- 链接：<https://arxiv.org/abs/2310.06770>

### 19. WebArena: A Realistic Web Environment for Building Autonomous Agents
- Zhou et al.（CMU）· ICLR 2024 · 引用约 2,298
- 自托管的真实网站环境（电商/论坛/GitLab 等）+ 功能级成功判定，成为 Web Agent 的标准试炼场。
- 链接：<https://arxiv.org/abs/2307.13854>

### 20. OSWorld: Benchmarking Multimodal Agents for Open-Ended Tasks in Real Computer Environments
- Xie et al. · NeurIPS 2024 · 引用约 1,478
- 真实操作系统环境（Ubuntu/Windows/macOS）中的开放式任务基准，Computer Use Agent 的核心评测。
- 链接：<https://arxiv.org/abs/2404.07972>

### 21. GAIA: A Benchmark for General AI Assistants
- Mialon et al.（Meta/HF）· ICLR 2024 · 引用约 1,344
- 通用助理能力基准：需要推理、多模态、网页浏览、工具链组合的分级问题。Hugging Face 上配套公开排行榜，社区活跃度极高。
- 链接：<https://arxiv.org/abs/2311.12983>

### 22. AgentBench: Evaluating LLMs as Agents
- Liu et al.（THUDM）· ICLR 2024 · 引用约 1,343
- 最早的系统性 Agent 基准之一：8 类环境（OS/数据库/知识图谱/网页/家居/卡牌等）横向评测 25+ 模型，量化"通用 Agent 能力差距"。
- 链接：<https://arxiv.org/abs/2308.03688>

### 23. τ-bench: A Benchmark for Tool-Agent-User Interaction in Real-World Domains
- Yao et al.（Sierra）· 2024
- 航空/零售客服场景的动态用户交互基准：引入"pass^k"指标测**一致性**（同一任务跑 k 次全对才算稳）——把 Agent 评测从"能不能做对一次"推进到"稳不稳"。
- 链接：<https://arxiv.org/abs/2406.12045>

---

## 六、工程实践与设计模式（生产环境怎么建）

### 24. Andrew Ng — 《Agentic Design Patterns》系列信（The Batch, 2024）
- Andrew Ng · DeepLearning.AI，2024-03 起 · 社区传播最广的 Agent 科普系列
- 提出四大设计模式：**Reflection、Tool Use、Planning、Multi-Agent Collaboration**，并用 HumanEval 实验证明 GPT-3.5 + 反思循环可超过 GPT-4 零样本——"Agentic workflow 比模型升级更重要"论断的出处。
- 链接：<https://www.deeplearning.ai/the-batch/how-agents-can-improve-llm-performance/>

### 25. OpenAI — 《A Practical Guide to Building Agents》
- OpenAI 官方指南，2025-04 · 企业落地引用最多的官方手册
- 三大组件（模型/工具/指令）+ 编排模式（单 Agent→Manager→Decentralized）+ 分层护栏体系（相关性分类器、PII 过滤、工具防护、人工干预），并强调"先用最强模型建基线，再换小模型优化成本"。
- 链接：<https://cdn.openai.com/business-guides-and-resources/a-practical-guide-to-building-agents.pdf>

### 26. Google — 《Agents》白皮书（2024）与《Agents Companion》（2025）
- Wiesner et al. · Google · 工业界视角的 Agent 架构教科书
- 系统阐述 Agent = 模型 + 工具 + 编排层的认知架构，Extensions/Functions/Data Stores 三种工具形态，以及评估与护栏。Companion 版补充多智能体与 AgentOps。
- 链接：<https://www.kaggle.com/whitepaper-agents>

### 27. Hugging Face — 《smolagents: Building Agents with Code-First Design》
- Hugging Face Blog，2025-01
- 主张"让 Agent 用代码而非 JSON 表达动作"（CodeAgent）：组合性更强、token 更省。代表了与 ReAct/JSON function calling 不同的第三条构建路线。
- 链接：<https://huggingface.co/blog/smolagents>

### 28. SWE-agent: Agent-Computer Interfaces Enable Automated Software Engineering
- Yang et al.（Princeton）· NeurIPS 2024
- 提出 **ACI（Agent-Computer Interface）** 概念：为人设计的工具（如 shell/编辑器）对 Agent 并不友好，需要专门设计命令集与反馈格式。SWE-bench 上首个破 12% 的系统。
- 链接：<https://arxiv.org/abs/2405.15793>

### 29. HumanLayer — 《12-Factor Agents》
- Dex Horthy · 2025 · GitHub 高星工程原则清单
- 把 12-Factor App 思想移植到 Agent：拥有你的提示词、拥有你的上下文窗口、工具即结构化输出、错误进上下文、小 Agent 优于大 Agent 等 12 条——社区最推崇的生产级 Agent 工程纪律。
- 链接：<https://github.com/humanlayer/12-factor-agents>

### 30. Microsoft — 《Magentic-One: A Generalist Multi-Agent System for Solving Complex Tasks》
- Microsoft Research Blog，2024-11
- AutoGen 之上的通用多智能体系统实践：Orchestrator（规划/记账/纠偏）+ 四个专职 Agent（网页/文件/编码/终端）的职责分工设计，GAIA 等基准上达到当时 SOTA。
- 链接：<https://www.microsoft.com/en-us/research/blog/magentic-one-a-generalist-multi-agent-system-for-solving-complex-tasks/>

---

## 延伸阅读（未计入 30 篇但值得一看）

| 资料 | 说明 |
|---|---|
| ReAct（ICLR 2023）/ Reflexion（NeurIPS 2023） | Agent 基石，见《幻觉》清单第 13、15 篇 |
| Anthropic《Building Effective Agents》 | 工作流 vs Agent 的取舍框架，见《幻觉》清单第 29 篇 |
| 《Agentic Design Patterns》（Antonio Gulli, 2025） | 424 页开源电子书，21 个模式的目录级参考 |
| AgentVerse（ICLR 2024） | 多智能体协作与涌现行为研究平台 |
| SWE-bench Multimodal / SWE-bench Goes Live | SWE-bench 的多模态与防污染续作 |
| LangGraph / CrewAI 官方文档 | 两大主流编排框架的工程参考 |

---

**筛选说明**：以"构建技术"为主线——规划、工具、协作、进化、评测、工程六环；纯提示词技巧（CoT 原版等）、纯模型训练（RLHF 等）未单列。与前三个清单的分工：幻觉清单讲"防错"，上下文清单讲"输入管理"，记忆清单讲"跨会话沉淀"，本清单讲"系统怎么搭"。
