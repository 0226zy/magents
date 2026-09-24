# LLM Agent 记忆（Memory）：30 篇高质量讨论精选

> 从学术论文引用量、社区阅读量与内容质量三个维度综合筛选。
> 论文引用量为 Google Scholar 检索约数（截至 2026 年 9 月，会随时间增长）。
> 整理日期：2026-09-23 · 与《幻觉》《上下文管理》两份清单配套，构成 Agent 可靠性三部曲。

**导言**：LLM 本身是无状态的——每次调用都是一张白纸。所谓"记忆"，是围绕模型构建的一整套**写入、组织、检索、更新、遗忘**机制。业界目前通用的分类借自认知科学：**工作记忆**（当前上下文窗口）、**情景记忆**（发生过的事件与对话）、**语义记忆**（提炼出的事实与偏好）、**程序性记忆**（技能与行为模式）。本清单按"认知科学渊源 → 综述 → 记忆架构与系统 → 评估基准 → 工程实践与产品"五个板块组织。

---

## 一、奠基与认知科学渊源

### 1. Cognitive Architectures for Language Agents（CoALA）
- Sumers, Yao, Narasimhan, Griffiths 等 · 2023 · 引用约 977
- 把经典认知架构（Soar、ACT-R）的记忆分类系统引入 LLM Agent：区分工作记忆与长期记忆（情景/语义/程序），并定义了记忆的读写动作空间。是后来几乎所有 Agent 记忆讨论的**概念起点**。
- 链接：<https://arxiv.org/abs/2309.02427>

### 2. Generative Agents: Interactive Simulacra of Human Behavior
- Park et al.（Stanford/Google）· UIST 2023 · 引用约 7,966
- "斯坦福小镇"：25 个 Agent 在沙盒中自主生活两天。提出记忆三件套——**记忆流（memory stream）+ 检索打分（相关性×新近性×重要性）+ 反思（reflection）**，至今仍是情景记忆系统的设计模板。
- 链接：<https://arxiv.org/abs/2304.03442>

### 3. Voyager: An Open-Ended Embodied Agent with Large Language Models
- Wang et al.（NVIDIA 等）· 2023 · 引用约 3,505
- Minecraft 终身学习 Agent。其**技能库（skill library）**是"程序性记忆"的标志性实现：把学会的技能存为可执行代码，持续积累、索引和复用。
- 链接：<https://arxiv.org/abs/2305.16291>

### 4. ExpeL: LLM Agents Are Experiential Learners
- Zhao et al. · AAAI 2024
- 无需训练地从成功/失败轨迹中**提炼经验规则**存入记忆，并在推理时检索应用——"吃一堑长一智"的最早系统化实现之一。
- 链接：<https://arxiv.org/abs/2308.10144>

### 5. Memory Matters: The Need to Improve Long-Term Memory in LLM-Agents
- Packer et al.（MemGPT 团队前身工作）· AAAI-SS 2023 · 引用约 163
- 短小但影响深远的立场文章：论证上下文窗口永远不够，Agent 必须拥有显式的长期记忆子系统——为 MemGPT 路线定调。
- 链接：<https://ojs.aaai.org/index.php/AAAI-SS/article/view/27688>

---

## 二、综述（快速建立全景）

### 6. A Survey on the Memory Mechanism of Large Language Model based Agents
- Zhang et al. · ACM TOIS 2025 · 引用约 959
- Agent 记忆领域引用最高的综述：围绕记忆的**写入—管理—读取**全生命周期分类，覆盖文本/参数化/隐式记忆三种形态，含大量方法对比表。
- 链接：<https://arxiv.org/abs/2404.13581>

### 7. From Human Memory to AI Memory: A Survey on Memory Mechanisms in the Age of LLM
- 2025 · 引用约 157
- 以人类记忆心理学（感觉记忆→工作记忆→长期记忆，编码→巩固→检索）为轴线映射 AI 记忆机制，理论框架感强。
- 链接：<https://arxiv.org/abs/2504.15965>

### 8. Memory in the Age of AI Agents
- 2025 · 引用约 97
- 面向 Agent 时代的记忆综述，对"形式—功能—动态"三维分类和主流系统（MemGPT、A-MEM、Mem0、Zep 等）的比较做得较新。
- 链接：<https://arxiv.org/abs/2512.13564>

### 9. AI Meets Brain: A Unified Survey on Memory Systems from Cognitive Neuroscience to Autonomous Agents
- 2025-12
- 最新的认知神经科学×Agent 记忆交叉综述，覆盖从情景记忆巩固到 Agent 记忆演化的完整谱系。
- 链接：<https://arxiv.org/abs/2512.23343>

---

## 三、记忆架构与系统（核心方法与系统实现）

### 10. MemGPT: Towards LLMs as Operating Systems
- Packer et al.（UC Berkeley，后演化为 Letta）· 2023 · 引用约 1,729
- 记忆领域影响力最大的系统论文：借鉴操作系统虚拟内存思想——上下文窗口=内存，外部存储=磁盘，Agent 通过函数调用自主分页换入换出。三层结构（Core/Recall/Archival）至今是工程标配。
- 链接：<https://arxiv.org/abs/2310.08560>

### 11. Augmenting Language Models with Long-Term Memory（LONGMEM）
- Wang et al. · NeurIPS 2023 · 引用约 490
- 参数侧路线：把长文本的键值缓存进解耦的"记忆库"，用残差注意力读取，以极低成本把上下文扩展到 65K。
- 链接：<https://arxiv.org/abs/2306.07174>

### 12. MemoryBank: Enhancing Large Language Models with Long-Term Memory
- Zhong et al. · AAAI 2024 · 引用约 1,408
- 借鉴**艾宾浩斯遗忘曲线**设计记忆的强化与衰减：被反复检索的记忆增强，长期不用的衰减遗忘——让 Agent 在长期交互中保持"人格化"记忆。
- 链接：<https://arxiv.org/abs/2305.10250>

### 13. Recursively Summarizing Enables Long-Term Dialogue Memory in Large Language Models
- Wang et al. · 2023
- 摘要式记忆的经典基线：递归地把旧对话压缩成摘要再续接新对话，实现无限长度的对话记忆。简单但至今被广泛用作对照。
- 链接：<https://arxiv.org/abs/2308.15022>

### 14. MemoryLLM: Towards Self-Updatable Large Language Models
- Wang et al. · ICML 2024 · 引用约 143
- 把一小块可更新的记忆池**嵌入模型参数内部**，实现自我更新而不需完整重训——参数化记忆路线的代表作。
- 链接：<https://arxiv.org/abs/2402.04624>

### 15. HippoRAG: Neurobiologically Inspired Long-Term Memory for Large Language Models
- Gutierrez et al.（Ohio State）· NeurIPS 2024 · 引用约 982
- 模仿**海马体索引理论**：知识图谱存实体关系（新皮质）+ 个性化 PageRank 做联想检索（海马体索引），实现跨文档多跳记忆联想。
- 链接：<https://arxiv.org/abs/2405.14831>

### 16. Zep: A Temporal Knowledge Graph Architecture for Agent Memory
- Rasmussen et al.（Zep 团队）· 2025 · 引用约 428
- **时序知识图谱**记忆：每条事实是带有效期窗口（valid_at/invalid_at）的边，新事实自动闭合旧事实——解决"用户信息会变化"这个向量检索难以处理的问题。开源实现 Graphiti 约 3 万星。
- 链接：<https://arxiv.org/abs/2501.13956>

### 17. A-MEM: Agentic Memory for LLM Agents
- Xu et al. · NeurIPS 2025 · 引用约 1,154
- 借鉴**卡片盒笔记法（Zettelkasten）**：每条记忆自动生成上下文描述、关键词、标签，并动态建立链接、触发历史记忆演化。每次记忆操作约 1,200 token，比 MemGPT 路线省 85–93% token。
- 链接：<https://arxiv.org/abs/2502.12110>

### 18. Mem0: Building Production-Ready AI Agents with Scalable Long-Term Memory
- Chhikara et al. · 2025 · 引用约 989 · GitHub 约 6.5 万星
- 产业界采用最广的记忆层："抽取→巩固→检索"流水线 + 图变体 Mem0g。论文报告相比 OpenAI 内建记忆在 LOCOMO 基准上准确率 +26%、响应快 91%、token 省 90%。
- 链接：<https://arxiv.org/abs/2504.19413>

### 19. MemoryOS: Memory Operating System for AI Agents
- Kang et al.（BAAI 等）· EMNLP 2025 · 引用约 267
- 借鉴 OS 存储分层（短期/中期/长期）与"热度"淘汰策略，按页式管理对话记忆，在 LoCoMo 上显著优于 MemGPT。
- 链接：<https://aclanthology.org/2025.emnlp-main.1318/>

### 20. MemOS: A Memory OS for AI System
- Li et al.（记忆张量团队）· 2025 · 引用约 114
- 把记忆上升为操作系统级资源：统一的 MemCube 记忆表示（参数/激活/明文三种形态互转）、记忆调度与生命周期治理，代表"记忆系统化/基础设施化"的最新方向。
- 链接：<https://arxiv.org/abs/2507.03724>

### 21. MEM1: Learning to Synergize Memory and Reasoning for Efficient Long-Horizon Agents
- Zhou et al.（MIT 等）· ICLR 2026 · 引用约 242
- 用**强化学习训练模型边推理边写记忆**：长程任务中持续把状态压缩进内部记忆，上下文占用恒定而任务长度可扩展。代表"记忆管理能力可学习"的训练范式。
- 链接：<https://arxiv.org/abs/2506.15841>

### 22. Agentic Memory: Learning Unified Long-Term and Short-Term Memory Management
- Du et al. · ACL 2026 · 引用约 111
- 不再手写"何时写入/何时检索"规则，而是让模型通过训练**统一学习长短期记忆管理策略**，是 Memory-RL 方向的代表。
- 链接：<https://aclanthology.org/2026.acl-long.981/>

---

## 四、评估基准（记忆好不好，怎么测）

### 23. LongMemEval: Benchmarking Chat Assistants on Long-Term Interactive Memory
- Wu et al. · 2024（ICLR 2025）· 引用约 665
- 目前最常用的长期交互记忆基准：约 115K token 的历史会话 + 五类记忆问题（信息抽取、多轮综合、时间推理、知识更新、拒答）。是各记忆系统刷榜的标准战场。
- 链接：<https://arxiv.org/abs/2410.10813>

### 24. Evaluating Very Long-Term Conversational Memory of LLM Agents（LoCoMo）
- Maharana et al. · ACL 2024 · 引用约 975
- 超长对话（约 26K token/会话）记忆基准，涵盖单跳/多跳/时序/开放域/对抗性问题。发现长上下文直接读全历史 **不如** 好的记忆检索——为"记忆系统必要性"提供了关键证据。
- 链接：<https://aclanthology.org/2024.acl-long.747/>

### 25. From Recall to Forgetting: Benchmarking Long-Term Memory for Personalized Dialogue Agents
- ACL 2026 Findings · 引用约 22
- 反其道而行之的基准：不只测"记住"，还测**该忘的能否忘掉**（隐私信息、过期偏好）——记忆系统的"遗忘能力"评测开始受到重视。
- 链接：<https://aclanthology.org/2026.findings-acl.1337/>

---

## 五、工程实践与产品（生产环境怎么做记忆）

### 26. Anthropic — Memory Tool 与 Context Editing 官方文档
- Anthropic Platform Docs，2025-09 起
- 模型厂商提供的"官方记忆原语"：Memory Tool 让 Claude 通过文件式记忆工具自主创建/读取/更新/删除持久记忆；Context Editing 在服务端自动清理过期工具输出而不破坏 prompt 缓存。官方报告两者结合带来约 39% 的长任务性能提升。
- 链接：<https://platform.claude.com/docs/en/agents-and-tools/tool-use/memory-tool>

### 27. Letta — 《Benchmarking AI Agent Memory》
- Letta Blog，2025-08
- MemGPT 的开源产品化。三层记忆（Core/Recall/Archival）+ Agent 自编辑 + 心跳机制的完整实现，并公开了在 LOCOMO 上 74% 的实测结果与复现方法——少见的"敢公开基准"的记忆产品。
- 链接：<https://www.letta.com/blog/benchmarking-ai-agent-memory>

### 28. Mem0 — 官方博客与产品文档
- Mem0.ai，持续更新 · GitHub 约 6.5 万星
- 采用最广的独立记忆层的产品文档与博客：add/search 极简 API、用户/会话/Agent 三级作用域、记忆压缩引擎。适合作为"接入一个现成记忆层"的参考实现。
- 链接：<https://mem0.ai/>

### 29. LangChain LangMem — 记忆 SDK 与记忆概念文档
- LangChain，2025 起
- 把记忆组织为可配置的 Memory Manager：语义记忆（事实/画像）、情景记忆（以 few-shot 形式回放）、程序性记忆（用 Prompt Optimizer 根据对话结果改写 Agent 自己的指令）——三种记忆的落地形态都给出了。
- 链接：<https://langchain-ai.github.io/langmem/>

### 30. OpenAI — 《Memory and New Controls for ChatGPT》
- OpenAI 官方博客，2024-02 · 面向消费者产品的记忆设计范本
- ChatGPT 记忆的官方公告：记忆自主积累 + 用户可查看/删除的控制权设计。把"持久记忆"从研究概念变成亿级用户产品功能的标志性事件，也是讨论记忆**隐私与用户控制权**的必引案例。
- 链接：<https://openai.com/index/memory-and-new-controls-for-chatgpt/>

---

## 延伸阅读（未计入 30 篇但值得一看）

| 资料 | 说明 |
|---|---|
| Memory-RL（arXiv:2508.19828） | 用 RL 训练 Agent 主动管理和利用记忆 |
| O-Mem（arXiv:2511.13593） | 面向长时程自我进化 Agent 的全模态记忆系统 |
| MemTrack（arXiv:2510.01353） | 多平台动态环境下的长期记忆与状态跟踪评测 |
| ConvoMem Benchmark（arXiv:2511.10523） | 主张"前 150 轮对话不需要 RAG"的反直觉基准 |
| Reflexion（NeurIPS 2023） | 语言形式的 episodic memory 奠基作，见《幻觉》清单 |

---

**筛选说明**：优先收录"Agent 记忆机制"直接主题的文献；纯模型架构层面的 KV-Cache 压缩、RNN/线性注意力等"隐式记忆"工作仅在综述脉络中提及，未单列。与《上下文管理》清单的边界：上下文管理回答"当前窗口里放什么"，记忆系统回答"跨会话、跨任务沉淀什么"——两者通过"记忆检索结果注入上下文"衔接。
