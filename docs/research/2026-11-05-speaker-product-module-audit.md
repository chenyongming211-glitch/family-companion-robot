# 可直接复用的开源声纹服务核查（2026-11-05）

用户已明确：成熟开源模块在同条件下更好、运行与许可适合时，直接复用其识别核心，不自研替代算法。现有sherpa-onnx／ERes2Net本身也是开源核心；本轮新增代码只服务于SDK加载、隔离通信和评测。

## 实际产品模块：小智使用的 voiceprint-api

从小智后端的[声纹接入指南](https://github.com/xinnan-tech/xiaozhi-esp32-server/blob/4f6e9ec6b1169ac6f3dbd78b5d8b1f717e493b9b/docs/voiceprint-integration.md)定位到 `xinnan-tech/voiceprint-api`。本轮固定模块提交 `9ffadb1854900b2f7fa7e78978a363457083754f`，经官方GitHub连接器取得并重新计算9份源文件的Git blob SHA；原始材料留云端，未执行该服务或产生其识别率。

| 已核查内容 | 实际实现与含义 |
|---|---|
| 识别核心 | [服务代码](https://github.com/xinnan-tech/voiceprint-api/blob/9ffadb1854900b2f7fa7e78978a363457083754f/app/services/voiceprint_service.py)调用ModelScope的 `iic/speech_campplus_sv_zh-cn_3dspeaker_16k`；不是自主训练的新模型。本模型ID与本轮三个SDK／参考控制不同，不能用其它CAM++或SDK数字代替它的结果。 |
| 登记／识别业务 | 提取向量、读取候选、算余弦、取最大值并比较阈值；这些是已读服务层行为，不是完整模型内部或训练算法的还原。 |
| 模板管理 | [存储代码](https://github.com/xinnan-tech/voiceprint-api/blob/9ffadb1854900b2f7fa7e78978a363457083754f/app/database/voiceprint_db.py)以speaker_id存储一个向量，再次登记同ID覆盖旧值；没有自动保留多个设备模板的证据。 |
| 默认工作点 | [模块配置](https://github.com/xinnan-tech/voiceprint-api/blob/9ffadb1854900b2f7fa7e78978a363457083754f/app/core/config.py)回退0.2，小智当前接入指南的客户端参数默认0.4。它们属于不同配置层，实际部署需核对调用路径；都不是正确身份概率，也不能直接搬给base。 |
| 完整接入成本 | [README](https://github.com/xinnan-tech/voiceprint-api/blob/9ffadb1854900b2f7fa7e78978a363457083754f/README.md)提供注册／识别／删除服务，依赖Python和MySQL；[音频工具](https://github.com/xinnan-tech/voiceprint-api/blob/9ffadb1854900b2f7fa7e78978a363457083754f/app/utils/audio_utils.py)会生成临时WAV，数据库持久保存向量。整套搬用会带入这些运行及数据行为，需要与我们当前隔离、RAM评测和未来产品要求分别适配。 |
| 许可／成绩边界 | [代码许可](https://github.com/xinnan-tech/voiceprint-api/blob/9ffadb1854900b2f7fa7e78978a363457083754f/LICENSE)为Apache-2.0；这不替代模型、数据和下游产品条件核查。已读资料未提供与本项目相同录音、登记、候选池及陌生人工作点的可比准确率。 |

## 对本项目的取舍

“直接用开源模块”可减少模型和前端实现工作，但仍需接入登记／删除、候选成员、拒识及设备会话业务。只观察Demo能叫出名字，不能判断它是否同时多认了陌生人；需报告正确接纳、误归、拒识、陌生误接纳和延时。

本轮固定实测集合仍为官方SpeechBrain／Vosk SDK及WeSpeaker官方前端控制，使用同一B登记和标定政策；它衡量引擎接入效果，不冒称上述完整服务的默认行为。产品模块源码核查是补充材料，不增删本轮候选或改阈值。该服务若进入后续评测，需固定其模型版本并另测完整登记／识别路径；当前不宣布它更准，也不部署它。

云端证据根目录 `/opt/family-robot-validation/speaker-products-20261105-39ae72e`，来源、提交和9份文件摘要见 `evidence/product-module-source-audit.json`，源码保留在 `sources/product-audit/`。正式服务、模型、配置、固件、VAD／AEC和麦克风未因这次源码阅读改变。
