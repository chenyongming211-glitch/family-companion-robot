"""A bounded read phase that always leaves room for a model-authored answer."""


class DialogueTurn:
    MAX_READS = 3

    def __init__(self):
        self.reads = self.rounds = 0
        self.query_state = None
        self.history_keys = set()
        self.progress_requested = False
        self.final_only = False

    def allowed(self, names, state):
        names = set(names)
        if self.final_only or self.reads >= self.MAX_READS or self.rounds >= 4:
            return names & {'respond', 'ask_clarification'}
        # Compatibility tool remains callable by older clients/fixtures, but
        # new model turns can deliver replacement + body through respond once.
        names.discard('replace_content')
        if self.query_state == state:
            names.discard('query_content')
        return names

    def queried(self, state):
        if self.query_state == state or self.reads >= self.MAX_READS:
            return False
        self.query_state = state
        self.reads += 1
        return True

    def begin_history(self, key):
        if key in self.history_keys or self.reads >= self.MAX_READS:
            self.final_only = True
            return False
        self.history_keys.add(key)
        self.reads += 1
        return True
