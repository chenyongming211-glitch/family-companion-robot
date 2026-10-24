# Voice text recording implementation

> Execute inline with TDD on main. User authorizes text recording; no branches/worktrees, backend dependencies/tests/cloud deployment only on server.

**Goal:** 保存后续语音转写与助手输出，准确区分已播和生成稿，可通过SSH回看。

**Architecture:** 新增异步单写者JSONL日志，独立于SDK上下文。VoiceSession接最终转写，DrainVoiceSession接生成稿/原稿/settle；配置显式启用，CLI读取。私有正文始终在服务器。

- [x] 存储与读取：`family_voice/conversation_log.py`实现`ConversationLog.open(directory, source, on_failure)`、`append(kind, role, text, **metadata)`、`flush()`、`close(reason)`，顺序追加并fsync。`read_conversation(directory, id, after=0, limit=100)`分页，`list_conversations(directory, limit=10)`列会话。测试验证两条文字重读、0600/0700、队列/磁盘错误一次回调、尾行截断与路径拒绝。
- [x] 接线：Settings增加可选`text_record_dir`。Runtime开始/关闭日志，最终转写存`user.transcript`。ContentAgent存`assistant.generated`；DrainVoiceSession固定稿存`assistant.content`，play_content包装settle存`assistant.played`/`assistant.delivery_unknown`，提示同样覆盖。回归确认用户转写、原稿与完整播放分离，late回执归属仍正确，默认禁用不新增文件。
- [x] 读取入口：`python -m family_voice.conversation_log --directory DIR list`与`show ID --after N --limit N [--json]`；默认文本视图不把未播生成稿当真实回答。只通过已有SSH读取，不新增网页接口。
- [x] 云端先RED/GREEN，再全量Python；systemd增加受保护记录路径，部署仅独立18010/18020并核对实际文字保存。更新README/交接/验证记录，main提交推送，同步服务器源码。重开原生测试窗口并保留，不把旧上午快照混入新档案。
