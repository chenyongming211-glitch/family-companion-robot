# 聊天分段与归档读模型实施计划

> 使用subagent-driven-development逐任务实现/审查；已确认规则由用户“继续”授权执行。直接main提交/推送，不建分支或worktree。

**Goal:** 将有效交流按明确结束/30分钟空闲组织为稳定业务聊天，接通两端列表/详情并保留来源与播放证据。

**Architecture:** 新纯Go归约器+现有源只读适配+/chats接口，原/conversations兼容；确定性重放持久日志得到ID和归档状态，不增数据库/索引写入或正文副本。

**Tech Stack:** Go标准库、现有管理认证/安全读源/播放投影、Vue3/TS/CSS/Vite。

## Global Constraints

- Develop, commit and push directly on main; no branches or worktrees.
- All dependency installs, runtime tests/builds and service deploys run on the cloud server; Mac is code/Git and browser client only.
- No voice service/model/context/firmware/microphone/playback changes; supported firmware remains ESP32-S3.
- No credentials, raw audio, real record bodies or generated artifacts in Git/Mac files.
- Only body-authorized source-time household/device/binding data; old prebinding records are not imported or reassigned.
- Source facts and playback evidence remain unchanged; no source writes, cache body copies, new DB/deps/frameworks or summary model.
- Explicit end means existing client_stop; abort/normal close/disconnect are not explicit end. Unknown/pending state must not be guessed complete.

## Task 1: Pure lifecycle reducer

Files: new internal/conversation/lifecycle.go and lifecycle_test.go.

Interfaces (freeze for Task2):
```go
const PolicyVersion = 1
const IdleTimeout = 30*time.Minute
type Scope struct { FamilyID, DeviceID, BindingID string; BindingVersion int }
type Kind string // "admit", "settle", "close", "uncertain"
type Fact struct { Kind Kind; At time.Time; TurnID int64; Reason string }
type Source struct { ID string; Scope Scope; StartedAt time.Time; Facts []Fact }
type TurnRef struct { SourceID string; TurnID int64 }
type Chat struct {
 ID string; Scope Scope; StartedAt, LastActivityAt time.Time
 EndedAt *time.Time; ArchiveReason string
 Pending bool; Turns []TurnRef; SourceIDs []string
}
func Reduce(sources []Source, now time.Time) ([]Chat,error)
var ErrInvalid error
```
Scope comparable; sourceID32lowerhex, bindingversion>0. Fact Kind admit/settle requirepositiveTurnID; close reason client_stop orotherstring; uncertain noTurnID. Sources unique ID; facts nondecreasingat>=start and<=now, tie bysource start/ID/index. Budgetmax100000facts/10000acceptedturns. No transcript/files/auth/clock reads. HashchatID policy/scope/firstturn tuple using JSONmarshal canonicaltypedstruct or lengthprefix, fullSHA256.

Reducer rules exactly design: settle can precedeadmit, firstsettlementfreeze; pending preventsidle; repeatedadmit/settle ignore; advancebeforeeveryfact andfinalnow; thresholdinclusive; close pending unknown onlyonce andonlyclient_stoparchivesmappedsourcechat; oldsourceclosedchatneverendsnewchat. Emptynewsourceclient_stop mayendactiveonlyifsource.StartedAt>=chat.StartedAt. Uncertain markerholds source'slatestunarchivedchat, no newchat, no reopen; explicitclosecanstillarchive. Sourceclosedthenlateradmit invalid. No midnight logic. Return chats stable chronological order (adapter sortsnewestforpage).

- [x] Add cloudRED tests for basicadmit/settle/30min/reconnect andimplement reducer.
- [x] Add exactboundary/midnight/longpending/overlap/duplicate/settlebeforeadmit/late/explicitstopvsdisconnect/emptyclose/uncertain/crash/restartstable/scopes/invalid/budgets tests; cloud race/vet.
- [x] Spec+qualityreview, fix findings, commit task1.

Example:
```go
chats,err:=Reduce([]Source{{ID:strings.Repeat("a",32),Scope:scope,StartedAt:t0,Facts:[]Fact{{Kind:"admit",At:t0,TurnID:1},{Kind:"settle",At:t0.Add(time.Minute),TurnID:1}}}},t0.Add(31*time.Minute))
if err!=nil || len(chats)!=1 || chats[0].ArchiveReason!="inactivity" { t.Fatal("archive boundary") }
```
Cloud source /home/ubuntu/management-backend-20261109/source, GoPATH /home/ubuntu/management-backend-20261109/runtime/go/bin. Run `go test -race ./internal/conversation && go vet ./internal/conversation`.

## Task 2: Authorized source adapter and HTTP

Files: new internal/management/chats.go/chats_test.go/chats_http_test.go; modify types.go/server.go/audit.go only as needed. Preserve original records/memory contracts. No production voice code.

Interfaces: Reader.ListChats(accountID,scope string,f ConversationFilter)(ConversationPage,error); Reader.ReadChat(accountID,scope,id string,expected *string)(ConversationDTO,error). Use conversation.Task1 types. DTO additions perdesign plus sources arrayof{recordId,sessionId,startedAt}; TurnDTO sourceRecordID/sourceTurnID optional andIDsession:turn. Page.PendingSources optionalbool. New /chats methods samequeryboundsas/conversations; listrequiresbodygrant(allfilterbodygrantsnone403), detailsha64regex. Old APIsunchanged.

Validate bodygrantbeforeopenfile, filteronlyeligible sources/body; recheckmanifestidentityandboundedraw. ExistingprojectvalidatesJSONownership/playbackevidence andproducesTurnDTO; extractor derivesnormalizedfacts fromcompleteevents at(validboundeddecimaltimestamp). Onlyuser_text=>admit; validI2S terminalplayback=>settle; knownsent0generated/generated_interrupted/interrupt=>settle; drain_timeout=>settleunknown; no turn_error-alone/positivegenerated/progresssettle. First completeclosed eventpreferred, manifestfallback; mismatchfailure. Incomplete tail addsuncertainatlastcompleteevent timestamp(orstart); pagePendingSources. Readallbeforegroup/page;64MiB/100000events/10000turnbudget. CaptureNowafterread. No new persistentstate or source writes.

- [x] Add cloudRED reader/HTTP tests and implement normalized timeline with source-safeprojectreuse.
- [x] Add timing/eventordering/partial/legacy/bounds/foreign/no-body(no eventsopened)/binding/expecteddevice/restartID/source links/auditfailclosed tests.
- [x] Cloud race conversation+management+CLI, vet and candidatebuild; source review/fix/commit.

Run `go test -race ./internal/conversation ./internal/management ./cmd/management && go vet ./internal/conversation ./internal/management ./cmd/management && go build -o ../management-chat-candidate ./cmd/management`.

## Task 3: Live business-chat UI

Files: web/management/src/remote-api.ts/types.ts/pages/CorePages.vue and tests; mock API remains default.

Interfaces: live listConversationPage/listConversations use/chats. getConversation chooses/chats onlyforchat_64hex, otherwise/conversations sourcecompat; expecteddevicepreserved. ExtendoptionalDTOfieldsinclpendingSources andsource refs. No dependencies/newnavigationframework.

- [x] RED transport testnewlist/detail andsourcecompat; implement adapters/types.
- [x] Currentpage groupedbychatstartdate, showrounds/connections/status/pendingbanner; detailshowbusinessmetadataandsourcecollapsedlinks. Sourceconnection detaillabelclearlydifferent. Keepgenerated/playedASRtruth, errorhiding/pollinflight/cacheguards/mock.
- [x] Cloud28existing+newtests,typecheck/mock+livebuilds; source review/commit.
- [x] Browser synthetictwo-connectionsonechat/midnight/archive/source links/status/pagination/403503/refresh/mobile; realinitialempty andmockcore regression.

## Task 4: Release and handoff

- [x] Broad review, focusfixes; cloudsource/binary/frontendtraceable release, restartmanagementonly, keep18790mock and18791real+sameaccount/binding.
- [x] Verify actualempty/new-onlyscope, sourcefiles+voicePID/health/protectedhashesunchanged, no model/firmware/physicaloperation.
- [x] Updatehandoff/platformplan/README/verification; commit/pushmain, verifyremoteSHA/clean tree.
- [x] 用户实机步骤已明确保留待完成，不再请求当前操作；合成检查不计现场PASS。
- [ ] 用户后续实际发言/明确结束/重连后的新聊天分段与播放事实核对。

## 当前交付

后端4c2a309、前端b5f29e7，独立管理发布chat-20261109-r1。读模型/两端实现、云端与浏览器复核完成；现场验收及语音上下文接入仍待后续。证据见[验证记录](../../verification/2026-11-09-chat-archive.md)。
