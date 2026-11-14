package realtimestate

import (
	"math"
	"reflect"
	"strings"
	"testing"
)

func segmentTurn(generated string, sentences ...[]map[string]any) *Turn {
	turn := NewTurn(1)
	turn.Generated = generated
	turn.Sent, turn.Completed = 72000, 57600
	turn.PlaybackBasis = "i2s_dma"
	turn.Cut = true
	for i, words := range sentences {
		turn.Tracker.Add(map[string]any{"sentence_id": i + 1, "sentence_end": true, "words": words}, 0)
	}
	return turn
}

func segmentWord(text string, begin, end any) map[string]any {
	return map[string]any{"text": text, "begin_time": begin, "end_time": end}
}

func TestConfirmedPlaybackPreservesIndependentClausesAroundMismatch(t *testing.T) {
	for _, middle := range []string{"他找到了苹果。", "它找到了。"} {
		t.Run(middle, func(t *testing.T) {
			turn := segmentTurn("小兔走进森林。它找到了苹果。伙伴一起回家。", []map[string]any{
				segmentWord("小兔", 100, 250), segmentWord("走进森林。", 250, 600),
				segmentWord(middle, 700, 1100), segmentWord("伙伴一起回家。", 1300, 1700),
			})
			want := PlayedSegments{Segments: []ConfirmedSegment{{"小兔走进森林。", 100, 600}, {"伙伴一起回家。", 1300, 1700}}, PlayedSamples: 57600, Unknown: true}
			if got := ConfirmedPlayback(turn); !reflect.DeepEqual(got, want) {
				t.Fatalf("independently verified clauses lost or gap invented: got %+v, want %+v", got, want)
			}
		})
	}
}

func TestConfirmedPlaybackUsesSeparateASRWordPunctuation(t *testing.T) {
	words := []map[string]any{
		segmentWord("小兔", 100, 250), segmentWord("走进森林", 250, 600),
		segmentWord("他找到了苹果", 700, 1100), segmentWord("伙伴一起回家", 1300, 1700),
	}
	words[1]["punctuation"], words[2]["punctuation"], words[3]["punctuation"] = "。", "，", "。"
	turn := segmentTurn("小兔走进森林。它找到了苹果，伙伴一起回家。", words)
	want := []ConfirmedSegment{{"小兔走进森林。", 100, 600}, {"伙伴一起回家。", 1300, 1700}}
	if got := ConfirmedPlayback(turn); !reflect.DeepEqual(got.Segments, want) || !got.Unknown {
		t.Fatalf("actual ASR punctuation fields lost verified clauses: %+v", got)
	}
}

func TestConfirmedPlaybackDoesNotUseUnselectedASRPunctuation(t *testing.T) {
	words := []map[string]any{segmentWord("小兔出门", 0, 500), segmentWord("别的内容", 600, 1000)}
	words[1]["punctuation"] = "。"
	turn := segmentTurn("小兔出门。别的内容。", words)
	turn.Completed = 24000
	if got := ConfirmedPlayback(turn); len(got.Segments) != 0 || !got.Unknown {
		t.Fatalf("unselected punctuation completed an unfinished observed clause: %+v", got)
	}
}

func TestConfirmedPlaybackDoesNotTreatPunctuationFieldAsWords(t *testing.T) {
	word := segmentWord("小兔出门", 0, 500)
	word["punctuation"] = "不要回家。"
	turn := segmentTurn("小兔出门。不要回家。", []map[string]any{word})
	if got := ConfirmedPlayback(turn); len(got.Segments) != 1 || got.Segments[0].Text != "小兔出门。" {
		t.Fatalf("unvalidated punctuation supplied words: %+v", got)
	}
}

func TestConfirmedPlaybackRequiresClauseBoundaries(t *testing.T) {
	for _, tc := range []struct{ generated, observed string }{
		{"不要吃药。", "吃药。"},
		{"吃药。", "不要吃药。"},
		{"小兔找到了苹果。", "小兔找到了。"},
		{"小兔找到了。", "小兔找到了苹果。"},
		{"已经走到十七。", "已经走到17。"},
		{"它回家了。", "他回家了。"},
	} {
		t.Run(tc.generated+tc.observed, func(t *testing.T) {
			got := ConfirmedPlayback(segmentTurn(tc.generated, []map[string]any{segmentWord(tc.observed, 0, 500)}))
			if len(got.Segments) != 0 || !got.Unknown || got.Complete {
				t.Fatalf("unverified clause accepted: %+v", got)
			}
		})
	}
}

func TestConfirmedPlaybackDoesNotEraseNumericPunctuation(t *testing.T) {
	for _, tc := range []struct{ generated, observed string }{
		{"温度-5度。", "温度5度。"},
		{"温度+5度。", "温度5度。"},
		{"比例1/2。", "比例12。"},
		{"折扣5%。", "折扣5。"},
		{"温度3.14度。", "温度3。14度。"},
		{"温度3.14度。", "温度3,14度。"},
	} {
		t.Run(tc.generated+tc.observed, func(t *testing.T) {
			got := ConfirmedPlayback(segmentTurn(tc.generated, []map[string]any{segmentWord(tc.observed, 0, 500)}))
			if len(got.Segments) != 0 || !got.Unknown {
				t.Fatalf("meaningful numeric punctuation erased: %+v", got)
			}
		})
	}
}

func TestConfirmedPlaybackRejectsNumericCompoundSuffixAnchors(t *testing.T) {
	for _, tc := range []struct{ generated, suffix string }{
		{"比例1：2。", "2。"},
		{"比例1 : 2。", "2。"},
		{"比例1　：　2。", "2。"},
		{"比例１：２。", "２。"},
		{"比例一：二。", "二。"},
		{"比例1 : -2。", "-2。"},
		{"温度3 . 14度。", "14度。"},
		{"温度３．１４度。", "１４度。"},
		{"时间12 : 30。", "30。"},
		{"时间１２：３０。", "３０。"},
		{"重量1 , 000克。", "000克。"},
		{"重量１，０００克。", "０００克。"},
	} {
		t.Run(tc.generated, func(t *testing.T) {
			turn := segmentTurn(tc.generated, []map[string]any{segmentWord(tc.suffix, 0, 500)})
			if got := ConfirmedPlayback(turn); len(got.Segments) != 0 || !got.Unknown {
				t.Fatalf("numeric compound split into an unsafe suffix anchor: %+v", got)
			}
			turn = segmentTurn(tc.generated, []map[string]any{segmentWord(tc.generated, 0, 500)})
			want := []ConfirmedSegment{{tc.generated, 0, 500}}
			if got := ConfirmedPlayback(turn); !reflect.DeepEqual(got.Segments, want) {
				t.Fatalf("exact independently timed numeric compound lost: %+v", got)
			}
		})
	}
}

func TestConfirmedPlaybackKeepsChineseNumericSentenceBoundaries(t *testing.T) {
	for _, separator := range []string{"。", "，"} {
		t.Run(separator, func(t *testing.T) {
			turn := segmentTurn("他刚数到十七"+separator+"二楼传来敲门声。伙伴一起回家。", []map[string]any{
				segmentWord("他刚数到十七"+separator, 0, 300),
				segmentWord("二楼传来敲门生。", 400, 800),
				segmentWord("伙伴一起回家。", 900, 1200),
			})
			want := []ConfirmedSegment{{"他刚数到十七" + separator, 0, 300}, {"伙伴一起回家。", 900, 1200}}
			if got := ConfirmedPlayback(turn); !reflect.DeepEqual(got.Segments, want) || !got.Unknown {
				t.Fatalf("Chinese number near a sentence boundary merged independent clauses: %+v", got)
			}
		})
	}
}

func TestConfirmedPlaybackASRPunctuationCannotConstructNumericCompound(t *testing.T) {
	for _, tc := range []struct{ generated, first, punctuation, second string }{
		{"比例1：2。", "比例1", "：", "2"},
		{"比例1:2。", "比例1", ":", "2"},
		{"时间12：30。", "时间12", "：", "30"},
		{"温度3.14度。", "温度3", ".", "14度"},
		{"数量1,000。", "数量1", ",", "000"},
	} {
		t.Run(tc.generated, func(t *testing.T) {
			first, second := segmentWord(tc.first, 0, 200), segmentWord(tc.second, 300, 500)
			first["punctuation"], second["punctuation"] = tc.punctuation, "。"
			turn := segmentTurn(tc.generated, []map[string]any{first, second})
			if got := ConfirmedPlayback(turn); len(got.Segments) != 0 || !got.Unknown {
				t.Fatalf("punctuation metadata invented a numeric compound: %+v", got)
			}
			literal := segmentWord(strings.TrimSuffix(tc.generated, "。"), 0, 500)
			literal["punctuation"] = "。"
			turn = segmentTurn(tc.generated, []map[string]any{literal})
			want := []ConfirmedSegment{{tc.generated, 0, 500}}
			if got := ConfirmedPlayback(turn); !reflect.DeepEqual(got.Segments, want) {
				t.Fatalf("literal, timed numeric compound was not preserved: %+v", got)
			}
		})
	}
}

func TestConfirmedPlaybackRejectsZeroPlaybackAndZeroDurationWords(t *testing.T) {
	for _, tc := range []struct {
		name           string
		completed      int64
		beginMS, endMS float64
	}{
		{"unplayed zero endpoint", 0, 0, 0},
		{"guard zero endpoint", 1, 0, 0},
		{"played zero endpoint", 24000, 0, 0},
		{"played later zero duration", 24000, 500, 500},
	} {
		t.Run(tc.name, func(t *testing.T) {
			turn := segmentTurn("先出门。", []map[string]any{segmentWord("先出门。", tc.beginMS, tc.endMS)})
			turn.Completed = tc.completed
			if got := ConfirmedPlayback(turn); len(got.Segments) != 0 || !got.Unknown || got.PlayedSamples != tc.completed {
				t.Fatalf("unplayed or zero-duration text credited: %+v", got)
			}
		})
	}
}

func TestConfirmedPlaybackDoesNotFillZeroDurationWords(t *testing.T) {
	turn := segmentTurn("不要吃药。", []map[string]any{segmentWord("不要", 0, 0), segmentWord("吃药。", 100, 500)})
	if got := ConfirmedPlayback(turn); len(got.Segments) != 0 || !got.Unknown {
		t.Fatalf("zero-duration word supplied a missing clause prefix: %+v", got)
	}
}

func TestConfirmedPlaybackPreservesClausesAroundZeroDurationGap(t *testing.T) {
	turn := segmentTurn("先出门。然后吃饭。最后回家。", []map[string]any{
		segmentWord("先出门。", 0, 100), segmentWord("然后吃饭。", 200, 200), segmentWord("最后回家。", 300, 400),
	})
	want := []ConfirmedSegment{{"先出门。", 0, 100}, {"最后回家。", 300, 400}}
	if got := ConfirmedPlayback(turn); !reflect.DeepEqual(got.Segments, want) || !got.Unknown {
		t.Fatalf("zero-duration gap affected independent clauses: %+v", got)
	}
}

func TestConfirmedPlaybackRejectsRepeatedAndUnorderedAnchors(t *testing.T) {
	for _, tc := range []struct {
		name, generated string
		words           []map[string]any
	}{
		{"repeated draft", "回家了。回家了。", []map[string]any{segmentWord("回家了。", 0, 500)}},
		{"repeated observation", "回家了。", []map[string]any{segmentWord("回家了。回家了。", 0, 500)}},
		{"reordered", "小兔出门。小熊回家。", []map[string]any{segmentWord("小熊回家。", 0, 400), segmentWord("小兔出门。", 500, 900)}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := ConfirmedPlayback(segmentTurn(tc.generated, tc.words)); len(got.Segments) != 0 || !got.Unknown {
				t.Fatalf("ambiguous order guessed: %+v", got)
			}
		})
	}
}

func TestConfirmedPlaybackKeepsClausesOutsideCrossedAnchors(t *testing.T) {
	turn := segmentTurn("准备出门。小兔前进。小熊等候。最后回家。", []map[string]any{
		segmentWord("准备出门。", 0, 300), segmentWord("小熊等候。", 400, 700),
		segmentWord("小兔前进。", 800, 1100), segmentWord("最后回家。", 1200, 1600),
	})
	want := []ConfirmedSegment{{"准备出门。", 0, 300}, {"最后回家。", 1200, 1600}}
	if got := ConfirmedPlayback(turn); !reflect.DeepEqual(got.Segments, want) || !got.Unknown {
		t.Fatalf("crossed anchors affected independent clauses: %+v", got)
	}
}

func TestConfirmedPlaybackUsesMarginAndCanonicalSampleCount(t *testing.T) {
	turn := segmentTurn("先准备好。然后出门。最后回家。", []map[string]any{
		segmentWord("先准备好。", 0, 880), segmentWord("然后出门。", 880, 881), segmentWord("最后回家。", 1000, 1600),
	})
	turn.Completed = 24000 // 1000ms, only words ending at or before 880ms qualify.
	got := ConfirmedPlayback(turn)
	want := []ConfirmedSegment{{"先准备好。", 0, 880}}
	if !reflect.DeepEqual(got.Segments, want) || got.PlayedSamples != 24000 || !got.Unknown {
		t.Fatalf("margin or receipt sample count changed: %+v", got)
	}
	turn.Completed = 0
	if got := ConfirmedPlayback(turn); len(got.Segments) != 0 || got.PlayedSamples != 0 || !got.Unknown {
		t.Fatalf("unplayed words promoted: %+v", got)
	}
}

func TestConfirmedPlaybackDoesNotCompleteGuardTruncatedClause(t *testing.T) {
	turn := segmentTurn("小兔出门。它找到苹果。", []map[string]any{
		segmentWord("小兔出门。", 0, 300), segmentWord("它找到", 400, 600), segmentWord("苹果。", 700, 1000),
	})
	turn.Completed = 24000
	want := []ConfirmedSegment{{"小兔出门。", 0, 300}}
	if got := ConfirmedPlayback(turn); !reflect.DeepEqual(got.Segments, want) {
		t.Fatalf("incomplete trailing clause promoted: %+v", got)
	}
}

func TestConfirmedPlaybackValidatesObservationTimingAndContinuity(t *testing.T) {
	for _, tc := range []struct {
		name string
		word map[string]any
	}{
		{"missing begin", segmentWord("然后回家。", nil, 1000)},
		{"missing end", segmentWord("然后回家。", 700, nil)},
		{"nonfinite", segmentWord("然后回家。", 700, math.Inf(1))},
		{"backwards", segmentWord("然后回家。", 200, 1000)},
		{"beyond sent", segmentWord("然后回家。", 700, 4000)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			turn := segmentTurn("先出门。然后回家。", []map[string]any{segmentWord("先出门。", 0, 500), tc.word})
			want := []ConfirmedSegment{{"先出门。", 0, 500}}
			if got := ConfirmedPlayback(turn); !reflect.DeepEqual(got.Segments, want) || !got.Unknown {
				t.Fatalf("invalid later evidence affected safe clause: %+v", got)
			}
		})
	}
	turn := segmentTurn("先出门。然后回家。", []map[string]any{segmentWord("先出门。", 0, 500)})
	turn.Tracker.Add(map[string]any{"sentence_id": 3, "sentence_end": true, "words": []map[string]any{segmentWord("然后回家。", 700, 1000)}}, 0)
	if got := ConfirmedPlayback(turn); len(got.Segments) != 1 || got.Segments[0].Text != "先出门。" || !got.Unknown {
		t.Fatalf("recognition sentence gap crossed: %+v", got)
	}
}

func TestConfirmedPlaybackAcceptsFinalASRSentenceWithoutPunctuation(t *testing.T) {
	turn := segmentTurn("小兔出门。小熊回家。", []map[string]any{segmentWord("小兔出门", 0, 500)}, []map[string]any{segmentWord("小熊回家", 700, 1000)})
	want := []ConfirmedSegment{{"小兔出门。", 0, 500}, {"小熊回家。", 700, 1000}}
	if got := ConfirmedPlayback(turn); !reflect.DeepEqual(got.Segments, want) {
		t.Fatalf("final ASR sentence boundaries lost: %+v", got)
	}
	turn = segmentTurn("小兔出门。小熊回家。", []map[string]any{segmentWord("小兔出门小熊回家", 0, 1000)})
	if got := ConfirmedPlayback(turn); len(got.Segments) != 0 {
		t.Fatalf("punctuation-free merged clauses guessed: %+v", got)
	}
}

func TestConfirmedPlaybackDoesNotInventGeneratedClauseEnd(t *testing.T) {
	turn := segmentTurn("小兔出门。小熊回家", []map[string]any{segmentWord("小兔出门。小熊回家", 0, 1000)})
	if got := ConfirmedPlayback(turn); len(got.Segments) != 1 || got.Segments[0].Text != "小兔出门。" {
		t.Fatalf("unfinished generated tail treated as a whole clause: %+v", got)
	}
}

func TestConfirmedPlaybackPreservesStrictWholePlaybackContract(t *testing.T) {
	for _, basis := range []string{"", "CoreAudio_dataPlayedBack", "i2s_dma"} {
		for _, ended := range []bool{false, true} {
			for _, final := range []bool{false, true} {
				turn := NewTurn(1)
				turn.Generated = "全部说完。"
				turn.Sent, turn.Completed = 28800, 28800
				turn.Ended, turn.PlaybackFinal, turn.PlaybackBasis = ended, final, basis
				got := ConfirmedPlayback(turn)
				wantComplete := PlaybackEvidence(turn).Boundary == "complete_response"
				if got.Complete != wantComplete || got.PlayedSamples != 28800 {
					t.Fatalf("whole playback rule changed: basis=%q ended=%v final=%v got=%+v", basis, ended, final, got)
				}
				if wantComplete {
					if !reflect.DeepEqual(got.Segments, []ConfirmedSegment{{"全部说完。", 0, 1200}}) || got.Unknown {
						t.Fatalf("complete playback transcript unavailable: %+v", got)
					}
				} else if len(got.Segments) != 0 || !got.Unknown {
					t.Fatalf("missing terminal state guessed full text: %+v", got)
				}
			}
		}
	}
}

func TestConfirmedPlaybackRetainsFullTranscriptWithoutASRNormalization(t *testing.T) {
	turn := NewTurn(1)
	turn.Generated = "😊"
	turn.Sent, turn.Completed, turn.Ended = 14400, 14400, true
	want := []ConfirmedSegment{{"😊", 0, 600}}
	if got := ConfirmedPlayback(turn); !got.Complete || got.Unknown || !reflect.DeepEqual(got.Segments, want) {
		t.Fatalf("whole native transcript normalized away: %+v", got)
	}
	turn.Generated = ""
	if got := ConfirmedPlayback(turn); !got.Complete || !got.Unknown || len(got.Segments) != 0 {
		t.Fatalf("missing native transcript guessed: %+v", got)
	}
}

func TestConfirmedPlaybackKeepsBoundedContextAndDoesNotMutate(t *testing.T) {
	turn := segmentTurn(strings.Repeat("兔", 2049)+"。", []map[string]any{segmentWord("兔。", 0, 100)})
	turn.Ended, turn.PlaybackFinal, turn.Completed = true, true, turn.Sent
	if got := ConfirmedPlayback(turn); len(got.Segments) != 0 || !got.Unknown || !got.Complete {
		t.Fatalf("oversize transcript escaped bound: %+v", got)
	}
	turn = segmentTurn("先出门。", []map[string]any{segmentWord("先出门。", 0, 500)})
	turn.Tracker.Generated, turn.Tracker.TotalAudioMS = "untouched", 17
	first := ConfirmedPlayback(turn)
	first.Segments[0].Text = "caller changed result"
	if got := ConfirmedPlayback(turn); got.Segments[0].Text != "先出门。" || turn.Tracker.Generated != "untouched" || turn.Tracker.TotalAudioMS != 17 {
		t.Fatalf("pure extraction mutated shared state: %+v", got)
	}
}

func TestConfirmedPlaybackRejectsInvalidSamples(t *testing.T) {
	if got := ConfirmedPlayback(nil); len(got.Segments) != 0 || !got.Unknown || got.Complete || got.PlayedSamples != 0 {
		t.Fatalf("nil turn: %+v", got)
	}
	for _, counts := range [][2]int64{{-1, 0}, {100, -1}, {100, 101}, {0, 0}} {
		turn := segmentTurn("小兔出门。")
		turn.Sent, turn.Completed, turn.Ended = counts[0], counts[1], true
		if got := ConfirmedPlayback(turn); len(got.Segments) != 0 || !got.Unknown || got.Complete || got.PlayedSamples != 0 {
			t.Fatalf("invalid or empty counts accepted: %v %+v", counts, got)
		}
	}
}
