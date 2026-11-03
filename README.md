# AI_H_bloodontower · 血染钟楼：暗流涌动（在线版）

> **⚠️ 严禁商用**
>
> 本项目**纯为爱发电**，仅供学习、研究与个人娱乐使用。
> 禁止任何形式的商业用途，包括但不限于：售卖、付费部署、商业集成、企业内部使用、以本项目为基础提供付费服务。
> 未经作者书面许可，不得将本项目用于任何盈利场景。

---

## 项目简介

一个可自托管的《血染钟楼：暗流涌动》(Blood on the Clocktower — Trouble Brewing) 网页版，支持真人 + 大模型 AI 混战、纯 AI 上帝视角观战，以及完整的说书人流程。

- 服务端：Go + Gin + GORM (SQLite)
- 前端：Vue 3 + Element Plus（本地静态资源，无需外网 CDN）
- AI 服务：Python Flask，把各家的 OpenAI 兼容接口适配成统一的 AI 玩家行为

## 功能特性

### 对局
- 22 个官方角色（镇民 / 外来者 / 爪牙 / 恶魔），完整夜晚行动顺序与技能结算
- 昼夜阶段：夜晚 → 黎明 → 私聊 → 公聊 → 提名 → 夜晚
- 提名 / 辩论 / 投票 / 处决完整状态机，支持死亡票、管家限制等规则
- 真人交互选择：占卜师、僧侣、守鸦人、管家、投毒者、小恶魔、猎手
- 私聊会话（邀请 / 接受 / 拒绝 / 退出）、公聊、@ 提及
- 说书人信息、个人笔记（仅本地保存）、夜晚顺序表、角色技能表
- 座位标注：右键座位标注猜测角色 / 阵营 / 态度（仅自己可见）

### AI 玩家
- 座位级大模型配置，支持 DeepSeek / 智谱 / Kimi / 通义 / 混元 / 硅基流动 / Groq / OpenRouter / Ollama
- 同一房间可以多源混战（每个座位独立模型）
- AI 公聊、私聊、提名、投票、夜晚决策、猎手开枪
- 电子斗蛐蛐模式：纯 AI 对局 + 上帝视角观战（可看私聊、技能与角色）

### 其他
- 好友系统：搜索 / 申请 / 同意 / 删除 / 邀请入房
- 战绩与回放：个人战绩、历史对局回放
- 全服模型榜：按模型聚合所有对局，多维度评分（胜负、信息、逻辑、扮演、规范、稳定、互动）
- 响应式布局：桌面 / 手机自适应

## 目录结构

```
AI_H_bloodontower/
├── botc_server/            # Go 主服务（房间 Actor、引擎、WebSocket、静态资源）
│   ├── main.go
│   ├── internal/
│   │   ├── engine/         # 游戏规则引擎（角色、夜晚、提名、投票）
│   │   ├── room/           # 房间状态机、AI 调度、记录归档
│   │   ├── handler/        # HTTP/WS 处理器
│   │   ├── model/          # GORM 数据模型
│   │   ├── aiclient/       # AI 服务客户端
│   │   └── config/         # 配置
│   └── static/             # 前端（index.html / css / js / assets）
└── ai_service/             # Python AI 推理服务
    ├── main.py
    └── requirements.txt
```

## 快速开始

### 1. 启动 AI 服务（可选，纯真人对局可跳过）

```bash
cd ai_service
python -m venv venv
# Windows: venv\Scripts\activate
# Linux/macOS: source venv/bin/activate
pip install -r requirements.txt
AI_PORT=8842 python main.py
```

AI 的大模型 Key 在网页「房间设置 → 添加 AI 玩家」里按座位填写，不需要写进配置文件。

### 2. 编译并启动主服务

需要 Go 1.21+，并且启用 CGO（SQLite 驱动需要）：

```bash
cd botc_server
CGO_ENABLED=1 go build -o botc-server .
```

环境变量（可选）：

| 变量 | 默认值 | 说明 |
|---|---|---|
| `BOTC_ADDR` | `127.0.0.1:8741` | 监听地址 |
| `BOTC_AI_URL` | `http://127.0.0.1:8742` | AI 服务地址 |
| `BOTC_DB_PATH` | 可执行文件目录下 `data/botc.db` | SQLite 路径 |

```bash
BOTC_ADDR=127.0.0.1:8841 \
BOTC_AI_URL=http://127.0.0.1:8842 \
BOTC_DB_PATH=./data/botc.db \
./botc-server
```

打开 <http://127.0.0.1:8841>，注册账号后即可创建房间。

### 3. 玩一局

1. 创建房间 → 设定人数（5–12）
2. 添加 AI 玩家（选择服务商、填 API Key、测试连接）
3. 其他真人点「准备」，房主开始游戏
4. 或者直接进「电子斗蛐蛐」，加满 AI 看它们自己打

## 模型评分口径（模型榜）

所有房间、所有账号、所有模型的历史对局聚合统计：

| 维度 | 口径 |
|---|---|
| 胜负 | 胜率按小样本做贝叶斯收缩，避免 3 局全胜即满分 |
| 信息 | 发言中出现编号、数字、"X 号" 的比例 |
| 逻辑 | 因为 / 所以 / 因此 / 矛盾 / 证据 / 推断 等论证词比例 |
| 扮演 | 身份、技能、查验、怀疑、提名等角色化表述比例 |
| 规范 | 15–220 字，扣狼人杀术语、过短过长、复读 |
| 稳定 | 各局规范率的标准差，波动越大分数越低 |
| 互动 | @、#、编号指代等交互性发言比例 |

综合分 = 胜负 30% +（信息 + 逻辑）20% + 扮演 15% + 规范 15% + 稳定 10% + 互动 10%

## 版权与免责声明

- 《血染钟楼 / Blood on the Clocktower》版权归 **The Pandemonium Institute** 所有，本项目为非官方粉丝作品。
- 角色图标素材来自开源项目 [tomozbot/botc-icons](https://github.com/tomozbot/botc-icons)，版权归原作者 / 官方所有，仅用于学习演示。
- 本项目与官方无任何关联，不提供任何商业服务。

## 许可

**仅限非商业使用（Non-Commercial Only）。**

你可以：学习、修改、在非盈利场景自托管、分享给朋友一起玩。
你不可以：售卖、付费部署、商业集成、用于任何盈利产品或服务。

详见 [LICENSE](./LICENSE)。
