# UI 改版：参考 ima.qq.com 的工作台首页

## 状态
已完成（2026-09-27）。移交自 Codex 会话 `01a0e11f-b328-7ec1-a88b-1efeeebe3a07`，由 Claude Code 接续完成验收与提交。

## 交付内容
- 品牌图：`web/public/open-ima-icon.png`（品牌图标）+ `web/public/open-ima-mascot.png`（毛绒形象，首页主视觉），均为用户提供的图片
- `web/src/App.tsx`：新增 `HomeDeck`（hero 问答入口 + 导入/检索快捷操作 + insight 条）；品牌区改用图标图；新增入口只做导航，指向已有 tab
- `web/src/styles.css`：整体重写视觉系统（柔和米白/松绿/暖杏「本地知识工作台」），含响应式
- `web/scripts/mock-api.mjs`：补 `/api/settings` GET/PUT（对齐真实前端调用，仅用于本地验收）
- `docs/guide/common-pitfalls.md`：新增「无 origin/main 时从本地 main 建 worktree」坑位
- `web/dist` 产物随 commit 更新（Go embed 跟踪 dist）

## 验证记录
- `./scripts/harness.sh` 全绿（gofmt / go vet / go test / parser pytest 28 通过 / vitest 5 通过 / typecheck / 前后端 build）
- 浏览器实测（Playwriter + 本机 Chrome，mock API + Vite dev）：
  - 桌面 1440px：首页 hero、问答入口、insight 条、文档列表渲染正常
  - 点击验证：「问问…」→ 问答 tab、「检索片段」→ 搜索 tab、「配置中心」→ 设置面板，均无控制台错误
  - 手机 390px：无横向溢出；侧边抽屉可展开（遮罩/知识库列表/配置入口正常）
- 测试缺口：HomeDeck 无专门单测（现有 5 个 App 测试通过，未新增）；截图存于会话临时目录，未入库

## 过程备注
- 本 clone 无 remote，worktree 从本地 `main` 创建（已沉淀到 common-pitfalls）
- playwriter 扩展的页面工具栏会遮挡左上角汉堡按钮，需用 JS dispatch 点击验证移动端抽屉
