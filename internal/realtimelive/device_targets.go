package realtimelive

import (
	"strings"
	"unicode"
)

// This is a hardware scope check, not an action/intent classifier. The model
// still interprets quotations, hypotheticals and per-field negation. It cannot
// authorize substituting this socket for an explicitly different target.
func controlTargetOutcome(text string) string {
	if len(text) > 16384 {
		return "needs_clarification"
	}
	text = strings.ToLower(text)
	text = strings.NewReplacer(" but ", ",", " and ", ",", " then ", ",", "但是", ",", "不过", ",", "而是", ",", "然后", ",").Replace(text)
	clauses := strings.FieldsFunc(text, func(r rune) bool { return strings.ContainsRune(",，。;；!！?？\n\r.", r) })
	currentSeen, positiveCurrent, explicitPositiveCurrent, otherPositive, otherNegative := false, false, false, false, false
	for _, clause := range clauses {
		rawClause := clause
		clause = strings.Map(func(r rune) rune {
			if unicode.IsSpace(r) || r == '\'' || r == '’' {
				return -1
			}
			return r
		}, clause)
		current := containsControlPhrase(clause, []string{"当前设备", "当前的设备", "当前这台", "当前这一台", "当前手表", "当前音量", "当前亮度", "当前声音", "当前屏幕", "这台", "这个设备", "本机", "你自己", "你自己的", "你的音量", "你的亮度", "thisdevice", "currentdevice", "thisunit", "thiswatch", "thisspeaker", "yourvolume", "yourbrightness"})
		currentSeen = currentSeen || current
		if wholeCurrentDeviceProhibited(clause) {
			return "unsupported_target"
		}
		otherIndex, otherTarget := -1, ""
		for _, target := range []string{"另一台", "另外一台", "那一台", "那台", "那个设备", "其他设备", "其它设备", "别的设备", "另一个设备", "另外一个设备", "其他终端", "旁边设备", "旁边的设备", "旁边那", "另一部", "那部", "另一只", "另外一只", "另一块", "另外一块", "旁边的手表", "旁边的音箱", "另一个音箱", "第一台", "第二台", "第三台", "第1台", "第2台", "第3台", "第一个设备", "第二个设备", "第三个设备", "第一只手表", "第二只手表", "第三只手表", "一号设备", "二号设备", "三号设备", "1号设备", "2号设备", "3号设备", "微雪", "waveshare", "m5", "手机", "电脑", "计算机", "笔记本", "平板", "电视", "空调", "冰箱", "洗衣机", "扫地机", "台灯", "电灯", "灯泡", "家电", "插座", "加湿器", "净化器", "窗帘", "隔壁", "卧室", "客厅", "书房", "厨房", "浴室", "卫生间", "楼上", "楼下", "其他房间", "另一个房间", "otherdevice", "anotherdevice", "thatdevice", "thatwatch", "thatspeaker", "otherunit", "anotherunit", "otherwatch", "anotherwatch", "otherspeaker", "anotherspeaker", "otherterminal", "anotherterminal", "firstdevice", "seconddevice", "thirddevice", "deviceone", "devicetwo", "devicethree", "device1", "device2", "device3", "firstwatch", "secondwatch", "thirdwatch", "firstunit", "secondunit", "thirdunit", "nearbydevice", "adjacentdevice", "devicebeside", "phone", "computer", "laptop", "desktop", "tablet", "television", "airconditioner", "refrigerator", "fridge", "washingmachine", "lightbulb", "smartlight", "thermostat", "appliance", "airpurifier", "smartplug", "nextroom", "bedroom", "livingroom", "kitchen", "bathroom", "upstairs", "downstairs"} {
			if i := strings.Index(clause, target); i >= 0 && (otherIndex < 0 || i < otherIndex) {
				otherIndex, otherTarget = i, target
			}
		}
		// TV needs a word boundary: after whitespace normalization, "set
		// volume" also contains the letters "tv".
		for _, word := range strings.FieldsFunc(rawClause, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }) {
			if word == "tv" {
				i := strings.Index(clause, "tv")
				if otherIndex < 0 || i < otherIndex {
					otherIndex, otherTarget = i, "tv"
				}
			}
		}
		if otherIndex >= 0 {
			// A clearly negated other-target clause can be ignored only when
			// another clause explicitly and positively asks for this device.
			if otherControlClauseExcluded(clause, otherTarget) {
				otherNegative = true
			} else {
				otherPositive = true
			}
		} else if !controlClauseNegated(clause) && (current || deviceControlCandidate(clause)) {
			positiveCurrent = true
			explicitPositiveCurrent = explicitPositiveCurrent || (current && deviceControlCandidate(clause))
		}
	}
	if otherPositive {
		if currentSeen || positiveCurrent {
			return "needs_clarification"
		}
		return "unsupported_target"
	}
	if otherNegative && !explicitPositiveCurrent {
		if positiveCurrent {
			return "needs_clarification"
		}
		return "unsupported_target"
	}
	return ""
}

func containsControlPhrase(text string, phrases []string) bool {
	for _, phrase := range phrases {
		if strings.Contains(text, phrase) {
			return true
		}
	}
	return false
}

// Fixed patterns for actual negative control actions. A bare 别/不要/don't
// would misclassify "别忘了调手机" as permission to target this socket.
var scopeNegatedActions = func() []string {
	var actions []string
	for _, negation := range []string{"不要", "别", "不许", "禁止", "不能", "请勿", "勿", "不"} {
		for _, verb := range []string{"改动", "调整", "调节", "操作", "修改", "改变", "设置", "控制", "调", "动", "碰", "改"} {
			for _, modifier := range []string{"", "再", "先", "继续"} {
				actions = append(actions, negation+modifier+verb)
			}
		}
	}
	return append(actions, "dontchange", "donotchange", "dontadjust", "donotadjust", "dontset", "donotset", "donttouch", "donottouch", "dontmodify", "donotmodify", "dontcontrol", "donotcontrol", "dontmake", "donotmake", "neverchange", "neverset", "neveradjust", "avoidchanging", "avoidsetting")
}()

func controlClauseNegated(clause string) bool {
	return containsControlPhrase(clause, scopeNegatedActions)
}

func otherControlClauseExcluded(clause, target string) bool {
	remaining, directExclusion := clause, false
	for _, action := range scopeNegatedActions {
		for _, possessor := range []string{"", "我的", "你的", "我", "你", "my", "the", "your"} {
			if strings.Contains(clause, action+possessor+target) || strings.Contains(clause, target+action) || strings.Contains(clause, target+"先"+action) {
				directExclusion = true
			}
		}
		remaining = strings.ReplaceAll(remaining, action, "")
	}
	// These Chinese nouns are not action tokens: 设备 contains 设 and 空调
	// contains 调. Never strip arbitrary target substrings: normalized English
	// "set volume" contains "tv", and its positive setter must remain visible.
	for _, noun := range []string{"设备", "空调"} {
		remaining = strings.ReplaceAll(remaining, noun, "")
	}
	// Excluding one field never excludes a positive request for a different
	// field on that same other device. Decline both action orders conservatively.
	return directExclusion && !containsControlPhrase(remaining, []string{"调", "改", "动", "碰", "操作", "设置", "控制", "设", "set", "change", "adjust", "modify", "touch", "control", "turn", "make"})
}
func wholeCurrentDeviceProhibited(clause string) bool {
	current := containsControlPhrase(clause, []string{"当前", "这台", "这个设备", "本机", "你自己", "thisdevice", "currentdevice", "thisunit", "thiswatch", "thisspeaker"})
	// An explicit all-settings prohibition remains whole-device authority
	// even when it gives volume/brightness as examples.
	wholeBanSyntax := controlClauseNegated(clause) || containsControlPhrase(clause, []string{"不要对", "别对", "不能对", "禁止对", "请勿对", "不许对", "不要给", "别给"})
	if current && wholeBanSyntax && containsControlPhrase(clause, []string{"任何设置", "所有设置", "全部设置", "所有参数", "全部参数", "任何改动", "任何修改", "任何改变", "任何操作", "所有操作", "任何东西", "allsettings", "anysettings", "anysetting", "anything", "anychanges", "allchanges", "allparameters"}) {
		return true
	}
	// "音量别动，亮度设30" does not prohibit the whole device. Per-field
	// choices remain the planner's semantic responsibility.
	if containsControlPhrase(clause, []string{"音量", "声音", "亮度", "屏幕", "小声", "大声", "静音", "volume", "brightness", "screen", "speaker", "sound", "mute"}) {
		return false
	}
	if current && containsControlPhrase(clause, []string{"保持原样", "保持不变", "原样不动", "保持现状"}) {
		return true
	}
	for _, target := range []string{"当前这台", "当前设备", "这台设备", "这个设备", "当前", "这台", "本机", "你自己"} {
		for _, negation := range []string{"不要", "别", "不许", "禁止", "不能", "请勿", "勿", "不"} {
			for _, verb := range []string{"动", "碰", "改", "改动", "调整", "调节", "操作", "修改", "改变", "控制"} {
				for _, modifier := range []string{"", "再", "先", "继续"} {
					prohibition := negation + modifier + verb
					if strings.Contains(clause, prohibition+target) || strings.Contains(clause, target+prohibition) || strings.Contains(clause, target+"先"+prohibition) {
						return true
					}
				}
			}
		}
	}
	for _, target := range []string{"thisdevice", "thecurrentdevice", "currentdevice", "thisunit", "thiswatch", "thisspeaker"} {
		for _, prefix := range []string{"dontchange", "donotchange", "donttouch", "donottouch", "dontadjust", "donotadjust", "dontmodify", "donotmodify", "dontcontrol", "donotcontrol", "not"} {
			if strings.Contains(clause, prefix+target) {
				return true
			}
		}
		if strings.Contains(clause, "leave"+target+"unchanged") {
			return true
		}
	}
	return false
}
