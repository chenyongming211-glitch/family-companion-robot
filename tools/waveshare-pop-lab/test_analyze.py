import unittest
import numpy as np
from analyze import channel_stats

class SignalTests(unittest.TestCase):
    def test_clean_tone_has_no_impulse_windows(self):
        x=3000*np.sin(2*np.pi*400*np.arange(24000*3)/24000)
        d=channel_stats(x)
        self.assertEqual(d['large_residual_windows'],0)
        self.assertEqual(d['clipped_samples'],0)
    def test_single_impulse_is_visible(self):
        x=3000*np.sin(2*np.pi*400*np.arange(24000*3)/24000)
        x[30000]+=15000
        d=channel_stats(x)
        self.assertGreaterEqual(d['large_residual_windows'],1)
        self.assertGreater(d['residual_peak'],10000)

if __name__=='__main__':unittest.main()
