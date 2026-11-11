# 网页UI UX Pro Max及unslop验证

用户要求先UI UX Pro Max重新优化网页，再unslop去AI味。本批内部管理端和家庭网页已按此顺序实现、发布并验证；无新增业务写接口、小程序或固件改动。代码提交dc9a4db设计、28a9688 UI/UX、a7e4173 unslop/对比修正，文档收尾单独提交。

## 实现

- 灰白表面及原有陶橙强调色，统一4/8px间距、文字层级、44px控件、tabular数字与18px Lucide扁平线图标；不新增依赖/字体下载/营销图片。
- 家庭端桌面内容展开至1200px、设备与聊天双列；1024及以下顺序单列，手机保留底部导航，正文16px。设备面板不再随聊天列被拉高。
- 统计第三项使用现有devices.connection实际在线数量；离线/未知保持中性并有文字，不用“真实数据”常量暗示服务健康。
- unslop去掉通用欢迎标语与技术徽章，家庭标题为家庭设备；辅助页面目录移到内容后，保留全部链接。显示当前账号，设置入口改用实际用途名称，记忆详情保持记忆导航选中。
- 列表箭头靠右，设备占位符改同套图标；保留授权/绑定起点、7天24轮、播放/生成稿/未知、回执与清除说明。API、权限、失效清理、整10滑块提交和50秒回执逻辑未改。

## 自动验证与审查

- 两轮均在云端运行既有行为测试、typecheck/Vite构建。最终52通过、0失败/跳过；原同home hash登录/卸载后退出/迟到请求/控制回执等检查保持。标题变化只更新原认证行为用例的标题预期，没有弱化安全断言。
- 最后35份公开输入与Mac及提交a7e4173逐字节相同，验证前后未变；source manifest文件SHA `d53b73e7cd971e9a992f6cdae5a186e0227686dc5a1a84f79e4787cad289812a`。dist4 manifest文件SHA `274c7d95a0ac87f2ee9e7ec71b1d77b17af0a6015eb327323feec002b2aae75c`。
- 最终JS `main-BoApjjpn.js`，SHA `3e28c12733ee15274a702a5287e65f8d2a0a84c0bff1ec6b3a90b4c617121b6d`；CSS `main-zwbTY0Pc.css`，SHA `dd444106360b719752d02357e17686a59072d1954ca1e3389292d65b044212d2`。
- 独立审查发现2项P2：hover小字4.418:1、深侧栏焦点2.313:1。改muted为#626b65、sidebar焦点为#f0b58e，复审规格/质量PASS，无剩余发现。实际浏览器分别测得4.827及5.406，键盘focus-visible与solid outline成立。

## 真实浏览器

Ego TaskSpace10，顺序查看原版、UI/UX轮及最终unslop；截图仅总览/家庭首页，不保存聊天正文、密码或Cookie。相同URL的goto曾沿用旧document，已明确reload并等stats后核对实际bundle；加载占位画面不作为完成截图。

- admin/family分别在1440/1024/768/375共8种组合检查：无页面横向溢出，主要按钮/导航最低44px，375正文16px；桌面双列、其余单列。系统暗色偏好仍明确浅色主题，reduced-motion下transition0s，没有新增主题切换。
- 两端实际退出并在原home hash登录，均恢复正确标题/当前账号；当前设备1台微雪。控制只读20%/20%、2滑块22圆点、range最低44px；未拖动/提交真实控制。
- 两端真实聊天详情5轮、5用户及5助手面板，24近期记忆/详情、记忆父导航选中及实际来源聊天5轮均成功；内部审计50条、无错误。只输出数量/状态，不复制正文。
- 375家庭页滚动到底后辅助入口bottom708.1、底栏top739，入口可见，不被固定导航遮住。测试结束已清除视口/媒体模拟并保留两结果页给用户，原空间7未操作。

## 静态发布与保护

云端`/home/ubuntu/web-uiux-unslop-20261111`，仅用hash-pinned既有只读保护及原子静态发布，不启动本地/云端新服务。两轮发布及最终清理各自比较进程/argv/env、config含已结束M5绑定、records/.memory、权限/RW/socket/锁、Caddy字节/元数据、当前防火墙摘要与原审计前缀，均保持；允许真实读取追加审计。无实际音量/亮度设置、清除、USB或刷机。

管理仍1401849/b49d9554，Voice仍1161080/eab47a77；原18791与SSH保留。实际served HTML/JS/CSS SHA及no-store通过，当前目录仅最终4文件，删除4个无引用旧真实JS/CSS。最新公开来源pins为release父目录`web-uiux-unslop.json`；`real-only-frontend.json`为此前清理快照，不能拿旧pins覆盖本次授权更新。

原审计中断事务、原防火墙951e与c34c差异未关闭，本批不改规则或重设基线，strict_original_transaction_acceptance仍false。此前固件CI DNS故障没有由本批修复，不声明native构建或新实机/声学验收PASS。

## 来源

UI UX Pro Max用于设计/可访问性与Vue指导；其自动推荐营销/深色模板不符本产品，已按现有品牌及Flat Design/响应式规则选择。第二轮使用[unslop by Damir Sokolovsky](https://github.com/heydamir/unslop)，CC BY4.0，技能已安装至用户目录，后续对话也可使用。Lucide固定来源及ISC/MIT、unslop信用见[第三方说明](../../web/management/THIRD_PARTY_NOTICES.md)。计划见[实施计划](../superpowers/plans/2026-11-11-web-uiux-unslop.md)。
