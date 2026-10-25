"""Read private JSONL in place; emit aggregate comparisons, never audio/text."""
import argparse
import json
from pathlib import Path

from state import Turn, context_for


def replay(path):
    turns, snapshots = {}, {}
    for line in path.read_text().splitlines():
        event = json.loads(line)
        kind, identity = event['event'], event.get('id')
        if kind == 'input_segment':
            turns[identity] = Turn(identity)
        turn = turns.get(identity)
        if kind == 'user_text' and turn:
            turn.user = event['text']
        elif kind in ('generated', 'generated_interrupted') and turn:
            turn.generated = event['text']
            turn.sent = event['sent']
            turn.ended = kind == 'generated'
        elif kind == 'interrupt' and turn:
            turn.cut, turn.sent = True, event['sent']
            turn.completed = event['completed_before_receipt']
        elif kind == 'playback' and turn:
            turn.sent, turn.completed, turn.accepted = event['sent'], event['completed'], event['accepted']
        elif kind == 'output_asr_final' and turn:
            turn.tracker.add(event['sentence'], event['elapsed_ms'])
        elif kind == 'restored_context':
            prior = [t for i, t in turns.items() if i != identity]
            snapshots[identity] = {'before': event['data'], 'after': json.loads(context_for(prior))}
    return turns, snapshots


def summary(path):
    turns, snapshots = replay(path)
    recovered, compared = set(), 0
    for snapshot in snapshots.values():
        for old, new in zip(snapshot['before']['recent_turns'], snapshot['after']['recent_turns']):
            compared += 1
            if old.get('assistant_spoken_prefix') is None and new['playback_evidence']['spoken_text']:
                recovered.add(new['id'])
    return {'session': path.parent.name, 'turns': len(turns), 'snapshots': len(snapshots),
        'compared_entries': compared, 'recovered_response_ids': sorted(recovered)}


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('events', type=Path)
    args = parser.parse_args()
    print(json.dumps(summary(args.events), ensure_ascii=False))
