import test from 'node:test';
import assert from 'node:assert/strict';
import { analyzePublicPluginSource } from './inspect-desktop-static-metadata.mjs';

// Uses only the API present at e1ceddf7542163cfaacdc975a72387e51b2aeb82.
test('complete modern JavaScript retains public plugin literals', () => {
  const source = 'const matcher=/[[a]--[b]]/v; const id="browser@openai-curated-remote";';
  const result = analyzePublicPluginSource(source);
  assert.equal(result.complete, true,
    'complete modern JavaScript must retain the public identity after a v-flag regexp');
  assert.deepEqual(result.candidates.map(item => item.key), [
    'plugins.browser@openai-curated-remote.enabled',
    'plugins.browser@openai-curated-remote.auto_provision',
  ]);
});
