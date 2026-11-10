import test from 'node:test';
import assert from 'node:assert/strict';
import { verifyBindings } from '../verify-wxml.mjs';
test('native mustache rejects HTML entities inside if, elif and ordinary attributes', () => {
  for (const attribute of ['wx:if', 'wx:elif', 'disabled']) {
    assert.throws(() => verifyBindings(`<view ${attribute}="{{ready &amp;&amp; valid}}"></view>`), /native|binding|expression/i);
  }
});
test('binding extraction preserves >= operators and accepts normal text entities', () => {
  assert.doesNotThrow(() => verifyBindings('<view wx:if="{{familyIndex >= 0 && devices.length > 0}}" class="card {{ready ? \'yes\' : \'no\'}}">Tom &amp; Jerry</view>'));
  assert.doesNotThrow(() => verifyBindings('<view title="Tom &amp; Jerry" wx:elif="{{ready}}"></view>'));
});
test('every binding is parsed, including attributes after greater-than operators', () => {
  assert.throws(() => verifyBindings('<view wx:if="{{index >= 0}}" disabled="{{value +}}"></view>'), /native|binding|expression/i);
  assert.throws(() => verifyBindings('<view wx:elif="ready"></view>'), /native|binding|expression/i);
});
