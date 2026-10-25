"""Synthetic deterministic Python baseline; imports no service or credentials."""
import ast
import json
from pathlib import Path
import sys
from datetime import datetime as RealDatetime
from zoneinfo import ZoneInfo

root = Path(sys.argv[1])
sys.path.insert(0, str(root / 'tools/realtime-output-live'))
sys.path.insert(0, str(root / 'tools/realtime-output-probe'))
import alignment
import history
import identity
import state

class Clock:
    @staticmethod
    def now(zone):
        return RealDatetime(2026, 10, 25, 13, 14, 15, tzinfo=zone)

namespace = dict(datetime=Clock, ZoneInfo=ZoneInfo, capability_snapshot=identity.capability_snapshot, json=json)
tree = ast.parse((root / 'tools/realtime-output-live/server.py').read_text())
for node in tree.body:
    if (isinstance(node, ast.Assign) and any(isinstance(t, ast.Name) and t.id == 'INSTRUCTIONS' for t in node.targets)) or (isinstance(node, ast.FunctionDef) and node.name == 'instructions_for'):
        exec(compile(ast.Module(body=[node], type_ignores=[]), 'server.py', 'exec'), namespace)

def turn_from(data):
    turn = state.Turn(data['id'])
    for field in ('user', 'generated', 'sent', 'completed', 'accepted', 'cut', 'begun', 'ended'):
        if field in data:
            setattr(turn, field, data[field])
    for sentence in data.get('sentences', []) or []:
        turn.tracker.add(sentence, 1000)
    return turn

def evaluate(case):
    action = case['action']
    if action == 'number':
        return history.number_key(case['value'])
    if action == 'equivalent':
        return history.equivalent_prefix(case['generated'], case['observed'])
    if action in ('context', 'instructions'):
        context = state.context_for([turn_from(t) for t in case['turns']])
        if action == 'context':
            return json.loads(context)
        return namespace['instructions_for'](case['cfg'], context, connected=case['connected'])
    if action == 'tracker':
        tracker = alignment.PrefixTracker(case['generated'], case['total'])
        for entry in case['sentences']:
            tracker.add(entry['sentence'], entry['received'])
        return dict(candidate=tracker.candidate(case['played']), covers=tracker.covers(case['played']), first=tracker.first_usable_ms(case['played']))
    if action == 'receipt':
        turn=turn_from(case['turn'])
        if case.get('seal'):
            turn.seal_for_drain()
        accepted = [turn.receipt(e) for e in case['events']]
        return dict(accepted=accepted, completed=turn.completed, received=turn.accepted, cut=turn.cut, drain_target=turn.drain_target, drain_done=turn.drain_done.is_set(), can_send=turn.can_send())
    raise ValueError(action)

print(json.dumps([evaluate(c) for c in json.load(sys.stdin)], ensure_ascii=False, allow_nan=False))
