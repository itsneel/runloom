import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import test from 'node:test';

const styles = readFileSync(new URL('../src/styles.scss', import.meta.url), 'utf8');
const extract = block => Object.fromEntries([...block.matchAll(/--rl-([\w-]+):\s*(#[0-9a-f]+);/gi)].map(([, name, value]) => [name, value]));
const light = extract(styles.match(/:root\s*\{([^}]+)\}/s)[1]);
const dark = { ...light, ...extract(styles.match(/:root\[data-theme='dark'\]\s*\{([^}]+)\}/s)[1]) };
const luminance = (hex) => {
  assert.ok(hex, 'Theme token must exist');
  const digits = hex.slice(1);
  const expanded = digits.length === 3 ? [...digits].map(char => char + char).join('') : digits;
  const channels = expanded.match(/../g).map(value => {
    const channel = parseInt(value, 16) / 255;
    return channel <= 0.04045 ? channel / 12.92 : ((channel + 0.055) / 1.055) ** 2.4;
  });
  return channels[0] * 0.2126 + channels[1] * 0.7152 + channels[2] * 0.0722;
};

const pairs = [
  ...['ink', 'secondary', 'muted', 'accent'].flatMap(foreground => ['surface', 'canvas', 'soft'].map(background => [foreground, background])),
  ['on-accent', 'accent'], ['on-accent', 'accent-hover'], ['accent', 'accent-soft'], ['code-text', 'code-bg'],
  ['success', 'success-soft'], ['warning', 'warning-soft'], ['danger', 'danger-soft'],
  ...['nav-text', 'nav-muted'].flatMap(foreground => ['nav', 'nav-hover', 'nav-selected'].map(background => [foreground, background])),
];
for (const [theme, tokens] of Object.entries({ light, dark })) {
 for (const [foreground, background] of pairs) {
  test(`${theme}: ${foreground} text on ${background} meets WCAG AA normal-text contrast`, () => {
    const a = luminance(tokens[foreground]);
    const b = luminance(tokens[background]);
    const ratio = (Math.max(a, b) + 0.05) / (Math.min(a, b) + 0.05);
    assert.ok(ratio >= 4.5, `${ratio.toFixed(2)}:1 contrast; expected at least 4.5:1`);
  });
 }
}
test('Global theme respects reduced motion and does not fetch external fonts', () => {
  assert.match(styles, /prefers-reduced-motion:\s*reduce/);
  assert.match(styles, /animation:\s*none\s*!important/);
  assert.doesNotMatch(styles, /fonts\.googleapis|@import\s+url/);
});
