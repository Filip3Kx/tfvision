export const NODE_WIDTH = 260;
export const NODE_HEIGHT = 108;
export const X_GAP = 110;
export const Y_GAP = 38;

function resourceKey(resource) {
  return resource?.id || resource?.address || `${resource?.type || 'resource'}.${resource?.name || 'unknown'}`;
}

/**
 * Computes a hierarchical left-to-right layout for a set of Terraform resource
 * nodes, resolving dependency levels and positioning nodes to avoid overlaps.
 *
 * Returns { nodes, edges, width, height } where each node is enriched with
 * `key`, `x`, and `y` fields, and each edge has `from`, `to`, `x1`, `y1`,
 * `x2`, `y2` fields suitable for rendering SVG lines.
 */
export function computeNodeLayout(resources) {
  if (!resources || resources.length === 0) {
    return { nodes: [], edges: [], width: 900, height: 420 };
  }

  const byKey = new Map(resources.map((resource) => [resourceKey(resource), resource]));
  const addressToKeys = new Map();

  resources.forEach((resource) => {
    const key = resourceKey(resource);
    const address = resource.address || key;
    if (!addressToKeys.has(address)) {
      addressToKeys.set(address, []);
    }
    addressToKeys.get(address).push(key);
  });

  const levelMemo = new Map();

  const resolveLevel = (nodeKey, stack = new Set()) => {
    if (levelMemo.has(nodeKey)) return levelMemo.get(nodeKey);
    if (stack.has(nodeKey)) return 0; // break cycles

    stack.add(nodeKey);
    const node = byKey.get(nodeKey);
    const depKeys = (node?.depends_on || [])
      .flatMap((depAddress) => addressToKeys.get(depAddress) || [])
      .filter((depKey) => depKey !== nodeKey);

    if (depKeys.length === 0) {
      levelMemo.set(nodeKey, 0);
      stack.delete(nodeKey);
      return 0;
    }

    const level = Math.max(...depKeys.map((depKey) => resolveLevel(depKey, stack))) + 1;
    levelMemo.set(nodeKey, level);
    stack.delete(nodeKey);
    return level;
  };

  const grouped = new Map();
  resources.forEach((resource) => {
    const nodeKey = resourceKey(resource);
    const level = resolveLevel(nodeKey);
    if (!grouped.has(level)) grouped.set(level, []);
    grouped.get(level).push(resource);
  });

  const levels = [...grouped.keys()].sort((a, b) => a - b);
  const layoutNodes = [];
  let maxPerColumn = 0;

  levels.forEach((level) => {
    const column = grouped.get(level) || [];
    column.sort((a, b) => {
      const left = `${a.module_path || 'root'}|${a.address || ''}|${a.id || ''}`;
      const right = `${b.module_path || 'root'}|${b.address || ''}|${b.id || ''}`;
      return left.localeCompare(right);
    });
    maxPerColumn = Math.max(maxPerColumn, column.length);
    column.forEach((resource, row) => {
      layoutNodes.push({
        ...resource,
        key: resourceKey(resource),
        x: 32 + level * (NODE_WIDTH + X_GAP),
        y: 32 + row * (NODE_HEIGHT + Y_GAP),
      });
    });
  });

  const positioned = new Map(layoutNodes.map((node) => [node.key, node]));
  const edges = [];
  layoutNodes.forEach((node) => {
    (node.depends_on || []).forEach((depAddress) => {
      const sourceKeys = addressToKeys.get(depAddress) || [];
      sourceKeys.forEach((sourceKey) => {
        const source = positioned.get(sourceKey);
        if (!source) return;
        edges.push({
          from: sourceKey,
          to: node.key,
          x1: source.x + NODE_WIDTH,
          y1: source.y + NODE_HEIGHT / 2,
          x2: node.x,
          y2: node.y + NODE_HEIGHT / 2,
        });
      });
    });
  });

  return {
    nodes: layoutNodes,
    edges,
    width: Math.max(900, levels.length * (NODE_WIDTH + X_GAP) + 160),
    height: Math.max(420, maxPerColumn * (NODE_HEIGHT + Y_GAP) + 100),
  };
}
