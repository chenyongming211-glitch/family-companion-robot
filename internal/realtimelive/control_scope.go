package realtimelive

import (
	"regexp"
	"strings"
	"unicode"
)

// A conservative admission check for this final request, never played history.
// Unknown language stays in chat; the planner cannot expand this authority.
// Target checking remains separate and does not call this classifier.
type controlScope struct {
	query, queryVolume, queryBrightness bool
	volume, brightness                  bool
	relativeVolume, relativeBrightness  bool
}

var controlAbsolute = regexp.MustCompile(`(调到|调成|调为|设为|设置为|设置成|设成|设置到|设到|设置|设|到|to|at)[百分之%零〇一二两三四五六七八九十百0-9]+`)
var controlJointFields = regexp.MustCompile(`(音量[和、]亮度|亮度[和、]音量)(都|一起)?(调到|调成|调为|设为|设置为|设置成|设成|设置到|设到|设置|设)[百分之%零〇一二两三四五六七八九十百0-9]+`)
var controlSetter = regexp.MustCompile(`^(请|请你|帮我|麻烦|现在|先|再|把|将|给我|你|的|当前|这台|这个设备|本机|设备|屏幕|声音|音量|亮度|说话|说|自己|please|canyou|couldyou|set|turn|make|adjust|change|the|your|thisdevice|currentdevice|speaker|screen|volume|brightness|sound|speak|quieter|louder|brighter|dimmer|mute|too|loud|dark|to|up|down|a|little|bit|it|百分之|[零〇一二两三四五六七八九十百0-9%]|调到|调成|调为|设为|设置为|设置成|设成|设置到|设到|设置|设|调|提高|降低|增加|减少|变|一点|点|大|小|高|低|亮|暗|声|太|吵|了|静音|息屏|关掉|关闭|打开|开启|都|和|、|并|同时|一下|好吗|可以吗|吧|吗)+`)
var controlEnglishAction = regexp.MustCompile(`^(please)?(canyou|couldyou)?(set|turn|adjust|change|make|mute|speakquieter|speaklouder|quieter|louder|brighter|dimmer|volumeup|volumedown|tooloud|toodark)`)
var controlIndirectEnglish = regexp.MustCompile(`\b(how|why|if|suppose|said|repeat|imitate|quote|yesterday)\b`)
var controlReportedSpeech = regexp.MustCompile(`(说|告诉我)[\s,，:：]`)

func currentControlScope(text string) controlScope {
	var scope controlScope
	if len(text) == 0 || len(text) > 16384 {
		return scope
	}
	text = strings.ToLower(text)
	// Use the same whitespace definition as clause normalization without
	// changing the original text's clause boundaries.
	reportingText := strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return ' '
		}
		return r
	}, text)
	// A reporting/conditional prefix can govern later comma-separated text.
	// Do not recover supposed commands from inside that uncertain request.
	if containsControlPhrase(text, []string{"他说", "她说", "它说", "我说", "说过", "模仿", "复述", "转述", "引用", "朗读", "重复一遍", "昨天", "以前", "刚才", "曾经", "如何", "怎么", "原理", "为什么", "假如", "如果", "假设", "要是"}) || controlIndirectEnglish.MatchString(text) || controlReportedSpeech.MatchString(reportingText) {
		return scope
	}
	// Remove quoted spans before clause splitting, so a quoted comma cannot
	// expose the rest of a quotation as a new direct request.
	text = unquotedControlText(text)
	text = strings.NewReplacer(" but ", ",", " and ", ",", " then ", ",", "但是", ",", "不过", ",", "而是", ",", "然后", ",", "并且", ",", "同时", ",").Replace(text)
	deniedVolume, deniedBrightness := false, false
	for _, clause := range strings.FieldsFunc(text, func(r rune) bool { return strings.ContainsRune(",，。;；!！?？\n\r.", r) }) {
		clause = strings.Map(func(r rune) rune {
			if unicode.IsSpace(r) || r == '\'' || r == '’' {
				return -1
			}
			return r
		}, clause)
		if containsControlPhrase(clause, []string{"故事", "已经", "story", "usedto"}) {
			continue
		}
		volume := containsControlPhrase(clause, []string{"音量", "声音", "volume", "speaker", "sound", "小声一点", "小声点", "大声一点", "大声点", "太吵", "静音", "quieter", "louder", "tooloud", "mute"})
		brightness := containsControlPhrase(clause, []string{"亮度", "屏幕", "brightness", "screen", "亮一点", "暗一点", "息屏", "brighter", "dimmer", "toodark"})
		query := containsControlPhrase(clause, []string{"查询", "查看", "查一下", "查设备", "查当前", "读取", "多少", "多大", "几", "设备状态", "当前状态", "屏幕状态", "devicestatus", "currentstatus", "whatis", "whats", "check", "read", "show"})
		negated := controlClauseNegated(clause) || containsControlPhrase(clause, []string{"不要", "不许", "禁止", "请勿", "别", "勿", "不能", "dont", "donot", "never", "保持", "不变", "不动"})
		if negated {
			// Explicitly excluding a different target does not revoke a later
			// positive command for this device. The target filter calls only the
			// lexical candidate check, never currentControlScope.
			if controlTargetOutcome(clause) == "" {
				deniedVolume = deniedVolume || volume
				deniedBrightness = deniedBrightness || brightness
			}
			continue
		}
		queryVolume := containsControlPhrase(clause, []string{"音量", "volume"})
		queryBrightness := containsControlPhrase(clause, []string{"亮度", "brightness", "屏幕状态"})
		if query && (queryVolume || queryBrightness || containsControlPhrase(clause, []string{"设备状态", "当前状态", "屏幕状态", "devicestatus", "currentstatus"})) {
			scope.query = true
			scope.queryVolume = scope.queryVolume || queryVolume
			scope.queryBrightness = scope.queryBrightness || queryBrightness
			continue
		}
		if i := strings.Index(clause, "再把当前设备"); i > 0 {
			clause = clause[i+len("再"):]
		}
		// Restrict writes to a direct control phrase. Finding a setter token
		// somewhere inside arbitrary prose is not authorization.
		if controlSetter.FindString(clause) != clause {
			continue
		}
		absolute := controlAbsolute.MatchString(clause)
		if absolute && strings.HasSuffix(clause, "了") {
			continue
		}
		if !strings.ContainsFunc(clause, func(r rune) bool { return unicode.Is(unicode.Han, r) }) && !controlEnglishAction.MatchString(clause) {
			continue
		}
		volumeNames := []string{"音量", "声音", "volume", "speaker", "sound"}
		brightnessNames := []string{"亮度", "brightness"}
		if containsControlPhrase(clause, volumeNames) && !containsControlPhrase(clause, []string{"亮度", "brightness", "屏幕", "screen"}) {
			brightness = false
		}
		if containsControlPhrase(clause, brightnessNames) && !containsControlPhrase(clause, volumeNames) {
			volume = false
		}
		jointAbsolute := controlJointFields.MatchString(clause)
		volumeAbsolute := jointAbsolute || controlFieldAbsolute(clause, volumeNames, brightnessNames)
		brightnessAbsolute := jointAbsolute || controlFieldAbsolute(clause, brightnessNames, volumeNames)
		volumeRelative := containsControlPhrase(clause, []string{"小一点", "大一点", "调小", "调大", "调低", "调高", "小声", "大声", "太吵", "增加", "降低", "减少", "提高", "quieter", "louder", "tooloud", "turndown", "turnup", "volumedown", "volumeup"})
		brightnessRelative := containsControlPhrase(clause, []string{"亮一点", "暗一点", "调亮", "调暗", "调低", "调高", "变亮", "变暗", "太暗", "太亮", "增加", "降低", "减少", "提高", "dimmer", "brighter", "toodark", "turndown", "turnup"})
		// Size words can refer to screen or font dimensions; require the brightness field.
		brightnessRelative = brightnessRelative || (containsControlPhrase(clause, brightnessNames) && containsControlPhrase(clause, []string{"小一点", "大一点", "调小", "调大"}))
		mute := containsControlPhrase(clause, []string{"静音", "mute"})
		screenOff := containsControlPhrase(clause, []string{"息屏", "关闭屏幕", "关掉屏幕"})
		if (volume && (volumeAbsolute || volumeRelative)) || mute {
			scope.volume = true
			scope.relativeVolume = scope.relativeVolume || (volumeRelative && !volumeAbsolute && !mute)
		}
		if (brightness && (brightnessAbsolute || brightnessRelative)) || screenOff {
			scope.brightness = true
			scope.relativeBrightness = scope.relativeBrightness || (brightnessRelative && !brightnessAbsolute && !screenOff)
		}
	}
	if scope.volume && deniedVolume || scope.brightness && deniedBrightness {
		return controlScope{}
	}
	return scope
}

// An absolute value belongs only to the named field before the next field.
// An absolute volume beside relative brightness cannot waive its status read.
func controlFieldAbsolute(clause string, own, other []string) bool {
	start := len(clause)
	for _, name := range own {
		if i := strings.Index(clause, name); i >= 0 && i < start {
			start = i
		}
	}
	if start == len(clause) {
		return false
	}
	part := clause[start:]
	end := len(part)
	for _, name := range other {
		if i := strings.Index(part, name); i >= 0 && i < end {
			end = i
		}
	}
	return controlAbsolute.MatchString(part[:end])
}

func unquotedControlText(text string) string {
	var out strings.Builder
	var closing rune
	runes := []rune(text)
	for i, r := range runes {
		if closing != 0 {
			if r == closing {
				closing = 0
			}
			continue
		}
		switch r {
		case '“':
			closing = '”'
		case '‘':
			closing = '’'
		case '「':
			closing = '」'
		case '『':
			closing = '』'
		case '"':
			closing = '"'
		case '\'':
			if i > 0 && i+1 < len(runes) && unicode.IsLetter(runes[i-1]) && unicode.IsLetter(runes[i+1]) {
				out.WriteRune(r)
			} else {
				closing = '\''
			}
		default:
			out.WriteRune(r)
		}
	}
	return out.String()
}

func (s controlScope) any() bool { return s.query || s.volume || s.brightness }
func (s controlScope) permits(name string) bool {
	switch name {
	case "device_get_status":
		return s.any()
	case "device_set_volume":
		return s.volume
	case "device_set_brightness":
		return s.brightness
	}
	return false
}

func controlField(name string) string {
	switch name {
	case "device_set_volume":
		return "volume"
	case "device_set_brightness":
		return "brightness"
	}
	return ""
}

func (s controlScope) complete(written map[string]bool, queried bool) bool {
	return (!s.volume || written["volume"]) && (!s.brightness || written["brightness"]) && (!s.query || queried)
}
