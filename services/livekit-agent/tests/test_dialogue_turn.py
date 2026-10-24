import unittest

try:
    from family_voice.dialogue_turn import DialogueTurn
except ImportError:
    DialogueTurn = None


class DialogueTurnTests(unittest.TestCase):
    def turn(self):
        self.assertIsNotNone(DialogueTurn, 'per-turn read policy is missing')
        return DialogueTurn()

    def test_same_query_state_is_removed_but_new_playback_state_can_be_queried(self):
        turn = self.turn()
        names = {'respond', 'ask_clarification', 'query_content', 'conversation_history'}
        self.assertIn('query_content', turn.allowed(names, 'state1'))
        turn.queried('state1')
        self.assertNotIn('query_content', turn.allowed(names, 'state1'))
        self.assertIn('query_content', turn.allowed(names, 'state2'))

    def test_read_budget_reserves_a_final_spoken_answer(self):
        turn = self.turn()
        for index in range(3):
            self.assertTrue(turn.begin_history(('read', 'current', index, 0, '')))
        self.assertEqual(turn.allowed({'respond','ask_clarification','conversation_history','resume_content'}, 's'),
                         {'respond','ask_clarification'})
        self.assertFalse(turn.begin_history(('read', 'current', 99, 0, '')))

    def test_repeated_history_cursor_is_not_read_twice(self):
        turn = self.turn()
        key = ('search','previous',0,0,'小狐狸')
        self.assertTrue(turn.begin_history(key))
        self.assertFalse(turn.begin_history(key))
        self.assertEqual(turn.allowed({'respond','ask_clarification','conversation_history'}, 'state'),
                         {'respond','ask_clarification'})
