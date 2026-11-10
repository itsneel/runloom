import assert from 'node:assert/strict';
import test from 'node:test';
import {
  buildCanvas,
  edgeGeometry,
  zoomAt,
} from '../src/app/features/builder/components/canvas/canvas-layout.ts';

const steps = [
  {
    id: 'fork',
    type: 'parallel',
    branches: [
      { id: 'a', steps: [{ id: 'one', type: 'activity' }] },
      {
        id: 'b',
        steps: [{ id: 'two', type: 'condition', then: [{ id: 'yes', type: 'wait' }], else: [] }],
      },
    ],
  },
  { id: 'after', type: 'activity' },
];
test('layout positions do not change sequence, branches, joins or connection targets', () => {
  const before = structuredClone(steps),
    first = buildCanvas(steps);
  const moved = buildCanvas(steps, { fork: { x: -400, y: 500 }, one: { x: 200, y: -600 } });
  assert.deepEqual(steps, before);
  assert.deepEqual(moved.edges, first.edges);
  assert.deepEqual(moved.nodes.find((n) => n.key === 'fork').x, -400);
  assert(first.edges.some((e) => e.from === '@join:fork' && e.to === 'after'));
  assert(first.edges.some((e) => e.from === 'two' && e.to === '@join:two' && e.label === 'No'));
  const ids = first.nodes.map((n) => n.key);
  assert.equal(new Set(ids).size, ids.length);
  for (const e of first.edges) assert(ids.includes(e.from) && ids.includes(e.to));
});
test('zoom keeps the world point beneath the mouse stationary', () => {
  const pan = { x: -100, y: 80 },
    anchor = { x: 480, y: 220 };
  const next = zoomAt(pan, 0.5, 2, anchor);
  assert.equal((anchor.x - pan.x) / 0.5, (anchor.x - next.x) / 2);
  assert.equal((anchor.y - pan.y) / 0.5, (anchor.y - next.y) / 2);
});
test('connector geometry updates for moved cards, including backwards layout', () => {
  const from = { x: 500, y: 200 },
    to = { x: -200, y: -100 };
  const first = edgeGeometry(from, to);
  assert(first.path.startsWith('M 690 268 C'));
  assert.notEqual(edgeGeometry({ ...from, x: 550 }, to).path, first.path);
});
test('End nodes have no continuation edge and empty drafts remain addable', () => {
  const end = buildCanvas([{ id: 'end', type: 'end' }]);
  assert(!end.nodes.some((n) => n.key === '@finish'));
  const empty = buildCanvas([]);
  assert.equal(empty.edges[0].insert.index, 0);
});

test('AI decisions expose every named action and a mode-specific join', () => {
  for (const mode of ['single', 'multiple', 'agent']) {
    const scene = buildCanvas([
      {
        id: 'route',
        type: 'decision',
        decision: { mode },
        branches: [
          { id: 'respond', description: 'Routine', steps: [{ id: 'reply', type: 'activity' }] },
          { id: 'escalate', description: 'Complex', steps: [{ id: 'review', type: 'pause' }] },
        ],
      },
      { id: 'after', type: 'activity' },
    ]);
    assert(
      scene.edges.some((e) => e.from === 'route' && e.to === 'reply' && e.label === 'respond'),
    );
    assert(
      scene.edges.some((e) => e.from === 'route' && e.to === 'review' && e.label === 'escalate'),
    );
    assert(scene.edges.some((e) => e.from === '@join:route' && e.to === 'after'));
    assert.match(
      scene.nodes.find((n) => n.key === '@join:route').label,
      mode === 'agent' ? /Decide again/ : /selected actions/,
    );
  }
});

test('inserting a generated node never mixes saved coordinates into overlapping cards', () => {
  const original = [
    { id: 'wait', type: 'wait' },
    { id: 'done', type: 'end' },
  ];
  const old = buildCanvas(original),
    positions = Object.fromEntries(old.nodes.map((n) => [n.key, { x: n.x, y: n.y }]));
  const changed = [original[0], { id: 'callback', type: 'callback' }, original[1]];
  const scene = buildCanvas(changed, positions);
  assert.deepEqual(scene, buildCanvas(changed));
  for (let i = 0; i < scene.nodes.length; i++)
    for (const b of scene.nodes.slice(i + 1)) {
      const a = scene.nodes[i];
      assert(Math.abs(a.x - b.x) >= 206 || Math.abs(a.y - b.y) >= 152);
    }
  assert.deepEqual(
    changed.map((s) => s.id),
    ['wait', 'callback', 'done'],
  );
});
test('nested branches and uneven paths have separated cards and forward connections', () => {
  const nested = [
    {
      id: 'fork',
      type: 'parallel',
      branches: [
        {
          id: 'long',
          steps: [
            {
              id: 'inner',
              type: 'condition',
              then: [
                { id: 'yes', type: 'wait' },
                { id: 'yes-two', type: 'wait' },
              ],
              else: [{ id: 'no', type: 'wait' }],
            },
            { id: 'long-tail', type: 'wait' },
          ],
        },
        { id: 'short', steps: [{ id: 'short-step', type: 'wait' }] },
        { id: 'empty', steps: [] },
      ],
    },
    { id: 'done', type: 'end' },
  ];
  const scene = buildCanvas(nested);
  for (let i = 0; i < scene.nodes.length; i++)
    for (const b of scene.nodes.slice(i + 1)) {
      const a = scene.nodes[i];
      assert(
        Math.abs(a.x - b.x) >= 206 || Math.abs(a.y - b.y) >= 152,
        `${a.key} overlaps ${b.key}`,
      );
    }
  for (const edge of scene.edges) {
    const a = scene.nodes.find((n) => n.key === edge.from),
      b = scene.nodes.find((n) => n.key === edge.to);
    assert(b.x > a.x, `${edge.from} points backwards to ${edge.to}`);
  }
});

test('signal handler entry points are disconnected from main flow and have independent action paths', () => {
  const main = [
    { id: 'main-wait', type: 'wait' },
    { id: 'main-end', type: 'end' },
  ];
  const handlers = [
    {
      id: 'on-update',
      signal_name: 'customer.updated',
      steps: [
        { id: 'update-action', type: 'activity' },
        { id: 'handler-return', type: 'end' },
      ],
    },
  ];
  const scene = buildCanvas(main, {}, handlers);
  assert(scene.nodes.some((n) => n.kind === 'handler' && n.key === '@handler:on-update'));
  assert(scene.edges.some((e) => e.from === '@handler:on-update' && e.to === 'update-action'));
  assert(!scene.edges.some((e) => e.from === 'main-end' && e.to === '@handler:on-update'));
  assert(
    scene.nodes.find((n) => n.key === '@handler:on-update').y >
      scene.nodes.find((n) => n.key === 'main-wait').y + 136,
  );
});
