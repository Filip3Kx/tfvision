import React, { useEffect, useMemo, useState } from 'react';
import { diffLines } from 'diff';
import './App.css';
import * as api from './api';

const NODE_WIDTH = 260;
const NODE_HEIGHT = 108;
const X_GAP = 110;
const Y_GAP = 38;
const ANSI_ESCAPE_PATTERN = /\u001b\[[0-9;]*m/g;
const BRACKET_COLOR_CODE_PATTERN = /\[(?:\d{1,3}(?:;\d{1,3})*)m/g;

function displayName(obj) {
  return obj?.Name || obj?.name || obj?.ID || obj?.id || 'unnamed';
}

function resourceKey(resource) {
  return resource?.id || resource?.address || `${resource?.type || 'resource'}.${resource?.name || 'unknown'}`;
}

function normalizeLogLine(line) {
  return line
    .replace(ANSI_ESCAPE_PATTERN, '')
    .replace(BRACKET_COLOR_CODE_PATTERN, '')
    .replace(/\r/g, '');
}

function classifyLogLine(line) {
  const trimmed = line.trim();
  if (trimmed === '') return 'blank';
  if (/^error:|\berror\b|^\u2577|^\u2575/i.test(trimmed)) return 'error';
  if (/^warning:|\bwarning\b/i.test(trimmed)) return 'warning';
  if (/^apply complete!|^plan:\s+\d+ to add, \d+ to change, \d+ to destroy\.?$/i.test(trimmed)) return 'summary';
  if (/\b(will be created|creation complete|created)\b/i.test(trimmed) || /^\+/.test(trimmed)) return 'added';
  if (/\b(will be updated|updated in-place|modifying)\b/i.test(trimmed) || /^~/.test(trimmed)) return 'changed';
  if (/\b(will be destroyed|destroy complete|destroyed)\b/i.test(trimmed) || /^-/.test(trimmed)) return 'removed';
  if (/^terraform will perform the following actions:$/i.test(trimmed) || /^terraform used the selected providers/i.test(trimmed)) return 'heading';
  return 'context';
}

function formatRunLog(rawLog) {
  const text = rawLog || '';
  const lines = text.split('\n');
  return lines.map((line, index) => {
    const clean = normalizeLogLine(line);
    return {
      key: `log-${index}-${clean}`,
      text: clean,
      tone: classifyLogLine(clean),
    };
  });
}

function computeNodeLayout(resources) {
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
    if (stack.has(nodeKey)) return 0;

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

function App() {
  const [organizations, setOrganizations] = useState([]);
  const [selectedOrg, setSelectedOrg] = useState(null);
  const [workspaces, setWorkspaces] = useState([]);
  const [selectedWorkspace, setSelectedWorkspace] = useState(null);
  const [activeTab, setActiveTab] = useState('history');

  const [stateHistory, setStateHistory] = useState([]);
  const [resources, setResources] = useState([]);
  const [runs, setRuns] = useState([]);
  const [stateSummary, setStateSummary] = useState(null);
  const [selectedHistoryId, setSelectedHistoryId] = useState('');
  const [selectedHistoryDetail, setSelectedHistoryDetail] = useState(null);
  const [selectedRunId, setSelectedRunId] = useState('');
  const [selectedRunDetail, setSelectedRunDetail] = useState(null);
  const [diffFromId, setDiffFromId] = useState('');
  const [diffToId, setDiffToId] = useState('');
  const [stateDiff, setStateDiff] = useState(null);
  const [resourceFilter, setResourceFilter] = useState('');
  const [selectedResourceKey, setSelectedResourceKey] = useState('');

  const [loading, setLoading] = useState(true);

  useEffect(() => {
    loadOrganizations();
  }, []);

  useEffect(() => {
    if (stateHistory.length >= 2) {
      setDiffToId(stateHistory[0].ID);
      setDiffFromId(stateHistory[1].ID);
    } else if (stateHistory.length === 1) {
      setDiffToId(stateHistory[0].ID);
      setDiffFromId(stateHistory[0].ID);
    }
  }, [stateHistory]);

  useEffect(() => {
    const loadDiff = async () => {
      if (!selectedWorkspace || !diffFromId || !diffToId) {
        setStateDiff(null);
        return;
      }
      const data = await api.fetchStateDiff(selectedWorkspace.ID, diffFromId, diffToId);
      setStateDiff(data);
    };
    loadDiff();
  }, [selectedWorkspace, diffFromId, diffToId]);

  useEffect(() => {
    const loadHistoryDetail = async () => {
      if (!selectedWorkspace || !selectedHistoryId) {
        setSelectedHistoryDetail(null);
        return;
      }
      const detail = await api.fetchStateVersionSummary(selectedWorkspace.ID, selectedHistoryId);
      setSelectedHistoryDetail(detail);
    };
    loadHistoryDetail();
  }, [selectedWorkspace, selectedHistoryId]);

  useEffect(() => {
    const loadRunDetail = async () => {
      if (!selectedRunId) {
        setSelectedRunDetail(null);
        return;
      }
      const detail = await api.fetchRunDetail(selectedRunId);
      setSelectedRunDetail(detail);
    };
    loadRunDetail();
  }, [selectedRunId]);

  const loadOrganizations = async () => {
    try {
      setLoading(true);
      const orgs = await api.fetchOrgs();
      setOrganizations(orgs);
      if (orgs.length > 0) {
        await handleSelectOrg(orgs[0]);
      }
    } catch (err) {
      console.error('Failed to load organizations', err);
    } finally {
      setLoading(false);
    }
  };

  const handleCreateOrg = async () => {
    const name = prompt('Organization name');
    if (!name) return;

    try {
      await api.createOrg(name);
      await loadOrganizations();
    } catch (err) {
      alert('Failed to create organization');
    }
  };

  const handleSelectOrg = async (org) => {
    setSelectedOrg(org);
    setSelectedWorkspace(null);
    setStateHistory([]);
    setResources([]);
    setRuns([]);
    setStateSummary(null);
    setSelectedHistoryId('');
    setSelectedHistoryDetail(null);
    setSelectedRunId('');
    setSelectedRunDetail(null);
    setStateDiff(null);
    setResourceFilter('');
    setSelectedResourceKey('');

    try {
      const ws = await api.fetchWorkspaces(org.ID);
      setWorkspaces(ws);
    } catch (err) {
      console.error('Failed to load workspaces', err);
    }
  };

  const handleCreateWorkspace = async () => {
    if (!selectedOrg) return;
    const workspaceName = prompt('Workspace name');
    if (!workspaceName) return;

    try {
      await api.createWorkspace(selectedOrg.ID, workspaceName);
      const ws = await api.fetchWorkspaces(selectedOrg.ID);
      setWorkspaces(ws);
    } catch (err) {
      alert('Failed to create workspace');
    }
  };

  const handleSelectWorkspace = async (workspace) => {
    setSelectedWorkspace(workspace);
    setActiveTab('history');
    setResourceFilter('');
    setSelectedResourceKey('');
    setSelectedHistoryId('');
    setSelectedHistoryDetail(null);
    setSelectedRunId('');
    setSelectedRunDetail(null);

    try {
      const [history, graphResources, summary, workspaceRuns] = await Promise.all([
        api.fetchStateVersions(workspace.ID),
        api.fetchResources(workspace.ID),
        api.fetchStateSummary(workspace.ID),
        api.fetchRuns(workspace.ID),
      ]);
      setStateHistory(history);
      setResources(graphResources);
      setRuns(workspaceRuns);
      setStateSummary(summary);
      setSelectedHistoryId(history[0]?.ID || '');
      setSelectedRunId(workspaceRuns[0]?.id || '');
    } catch (err) {
      console.error('Failed to load workspace details', err);
    }
  };

  const filteredResources = useMemo(() => {
    const query = resourceFilter.trim().toLowerCase();
    if (!query) return resources;
    return resources.filter((resource) => {
      const haystack = [
        resource.address,
        resource.type,
        resource.name,
        resource.provider,
        resource.provider_source,
        resource.provider_version,
        resource.module_path,
        resource.id,
      ].join(' ').toLowerCase();
      return haystack.includes(query);
    });
  }, [resources, resourceFilter]);

  const layout = useMemo(() => computeNodeLayout(filteredResources), [filteredResources]);

  const selectedResource = useMemo(() => {
    if (!selectedResourceKey) return null;
    return resources.find((resource) => resourceKey(resource) === selectedResourceKey) || null;
  }, [resources, selectedResourceKey]);

  const diffRawPreview = useMemo(() => {
    if (!stateDiff?.raw) return '';
    return JSON.stringify(stateDiff.raw, null, 2);
  }, [stateDiff]);

  const selectedSummary = selectedHistoryDetail?.summary || stateSummary;

  const selectedRun = useMemo(() => {
    if (!selectedRunId) return null;
    return runs.find((run) => run.id === selectedRunId) || selectedRunDetail;
  }, [runs, selectedRunId, selectedRunDetail]);

  const selectedRunLogBody = useMemo(() => {
    if (!selectedRun) return '';
    return selectedRunDetail?.log_body || selectedRun.log_body || '';
  }, [selectedRun, selectedRunDetail]);

  const formattedRunLog = useMemo(() => formatRunLog(selectedRunLogBody), [selectedRunLogBody]);

  const selectedHistoryCode = useMemo(() => {
    if (!selectedHistoryDetail?.raw) return '';
    return JSON.stringify(selectedHistoryDetail.raw, null, 2);
  }, [selectedHistoryDetail]);

  const gitLikeDiffBlocks = useMemo(() => {
    if (!stateDiff?.raw) return [];

    const before = JSON.stringify(stateDiff.raw.before || {}, null, 2);
    const after = JSON.stringify(stateDiff.raw.after || {}, null, 2);

    return diffLines(before, after).flatMap((part) => {
      const lines = part.value.split('\n');
      if (lines[lines.length - 1] === '') lines.pop();
      return lines.map((line, index) => ({
        key: `${part.added ? 'a' : part.removed ? 'r' : 'c'}-${index}-${line}`,
        type: part.added ? 'added' : part.removed ? 'removed' : 'context',
        text: line,
      }));
    });
  }, [stateDiff]);

  return (
    <div className="app-container">
      <aside className="sidebar glass-panel">
        <div className="logo-container">
          <div className="logo-icon">S</div>
          <span className="logo-text">tfvision</span>
        </div>

        <div className="nav-section">
          <div className="nav-label">Organizations</div>
          <button className="small-btn" onClick={handleCreateOrg}>+ Add organization</button>
          {organizations.map((org) => (
            <button
              key={org.ID}
              className={`nav-item ${selectedOrg?.ID === org.ID ? 'active' : ''}`}
              onClick={() => handleSelectOrg(org)}
            >
              <span className="nav-dot"></span>
              {displayName(org)}
            </button>
          ))}
        </div>
      </aside>

      <main className="main-content">
        <header className="main-header glass-panel">
          <div className="breadcrumb">
            <span className="bc-item">State Platform</span>
            {selectedOrg && <span className="bc-sep">/</span>}
            {selectedOrg && <span className="bc-item">{displayName(selectedOrg)}</span>}
            {selectedWorkspace && <span className="bc-sep">/</span>}
            {selectedWorkspace && <span className="bc-item active">{displayName(selectedWorkspace)}</span>}
          </div>
          {selectedOrg && !selectedWorkspace && (
            <button className="action-btn primary" onClick={handleCreateWorkspace}>Add Workspace</button>
          )}
        </header>

        <div className="scroll-area">
          {!loading && organizations.length === 0 && (
            <div className="empty-guide glass-panel">
              <h3>No organizations yet</h3>
              <p>Create your first organization to start tracking state versions.</p>
            </div>
          )}

          {!selectedWorkspace ? (
            <div className="dashboard-view animate-fade-in">
              <div className="view-header">
                <h2>Workspaces</h2>
              </div>
              <div className="workspace-grid">
                {workspaces.map((ws) => (
                  <div key={ws.ID} className="workspace-card glass-panel" onClick={() => handleSelectWorkspace(ws)}>
                    <div className="card-top">
                      <div className="status-badge healthy">STATE</div>
                    </div>
                    <h3 className="card-title">{displayName(ws)}</h3>
                    <div className="card-stats">
                      <div className="stat">
                        <span className="stat-label">Execution</span>
                        <span className="stat-value">Local CLI</span>
                      </div>
                      <div className="stat">
                        <span className="stat-label">Terraform</span>
                        <span className="stat-value">{ws.TerraformVersion || '1.14.8'}</span>
                      </div>
                    </div>
                  </div>
                ))}

                {selectedOrg && workspaces.length === 0 && (
                  <div className="empty-guide glass-panel">
                    <h3>No workspaces in {displayName(selectedOrg)}</h3>
                    <p>Run Terraform in cloud mode and this workspace will be created automatically.</p>
                    <pre className="tf-snippet">{`terraform {
  cloud {
    hostname     = "${window.location.hostname}"
    organization = "${displayName(selectedOrg)}"
    workspaces { name = "my-workspace" }
  }
}`}</pre>
                  </div>
                )}
              </div>
            </div>
          ) : (
            <div className="workspace-detail animate-fade-in">
              <div className="detail-header">
                <button className="back-btn" onClick={() => setSelectedWorkspace(null)}>Back to Workspaces</button>
                <h2>{displayName(selectedWorkspace)}</h2>
              </div>

              <div className="tab-nav glass-panel">
                <button className={`tab-btn ${activeTab === 'history' ? 'active' : ''}`} onClick={() => setActiveTab('history')}>State History</button>
                <button className={`tab-btn ${activeTab === 'diff' ? 'active' : ''}`} onClick={() => setActiveTab('diff')}>State Diff</button>
                <button className={`tab-btn ${activeTab === 'runs' ? 'active' : ''}`} onClick={() => setActiveTab('runs')}>Runs</button>
                <button className={`tab-btn ${activeTab === 'resources' ? 'active' : ''}`} onClick={() => setActiveTab('resources')}>Resource Canvas</button>
              </div>

              {activeTab === 'history' && (
                <section className="detail-section glass-panel">
                  <div className="section-header">State Versions</div>
                  {selectedSummary && (
                    <div className="state-summary">
                      <div className="summary-grid">
                        <div className="summary-card"><span>Terraform</span><strong>{selectedSummary.terraform_version || selectedWorkspace.TerraformVersion || 'Unknown'}</strong></div>
                        <div className="summary-card"><span>Serial</span><strong>{selectedSummary.serial ?? 'n/a'}</strong></div>
                        <div className="summary-card"><span>Resources</span><strong>{selectedSummary.resource_count ?? 0}</strong></div>
                        <div className="summary-card"><span>Modules</span><strong>{selectedSummary.module_count ?? 0}</strong></div>
                        <div className="summary-card"><span>Providers</span><strong>{selectedSummary.provider_count ?? 0}</strong></div>
                        <div className="summary-card"><span>Outputs</span><strong>{selectedSummary.output_count ?? 0}</strong></div>
                      </div>

                      <div className="summary-lists">
                        <div className="summary-list">
                          <h4>Modules</h4>
                          <ul>
                            {(selectedSummary.modules || []).slice(0, 12).map((moduleName) => (
                              <li key={`module-${moduleName}`}>{moduleName}</li>
                            ))}
                            {(selectedSummary.modules || []).length === 0 && <li>None</li>}
                          </ul>
                        </div>

                        <div className="summary-list">
                          <h4>Providers</h4>
                          <ul>
                            {(selectedSummary.providers || []).slice(0, 12).map((provider) => (
                              <li key={`${provider.source || provider.name || 'provider'}-${provider.version || 'unknown'}`}>
                                <span>{provider.source || provider.name || 'unknown'}</span>
                                <strong>{provider.version || 'unknown'}</strong>
                              </li>
                            ))}
                            {(selectedSummary.providers || []).length === 0 && <li>None</li>}
                          </ul>
                        </div>

                        <div className="summary-list">
                          <h4>Outputs</h4>
                          <ul>
                            {(selectedSummary.outputs || []).slice(0, 12).map((outputName) => (
                              <li key={`output-${outputName}`}>{outputName}</li>
                            ))}
                            {(selectedSummary.outputs || []).length === 0 && <li>None</li>}
                          </ul>
                        </div>
                      </div>

                      {selectedSummary.lineage && <p className="summary-lineage">Lineage: {selectedSummary.lineage}</p>}
                    </div>
                  )}

                  {stateHistory.length === 0 ? (
                    <div className="empty-guide">
                      <h3>No state versions yet</h3>
                      <p>Run terraform init/apply in this workspace to upload state snapshots.</p>
                    </div>
                  ) : (
                    <div className="timeline">
                      {stateHistory.map((sv, index) => (
                        <button
                          key={sv.ID}
                          className={`timeline-item ${selectedHistoryId === sv.ID ? 'active' : ''}`}
                          onClick={() => setSelectedHistoryId(sv.ID)}
                        >
                          <div className="timeline-marker"></div>
                          <div className="timeline-content">
                            <span className="sv-serial">v{sv.Serial}</span>
                            <span className="sv-date">{new Date(sv.CreatedAt).toLocaleString()}</span>
                            <span className="sv-lineage">{sv.Lineage}</span>
                            {index === 0 && <span className="current-tag">CURRENT</span>}
                          </div>
                        </button>
                      ))}
                    </div>
                  )}

                  {selectedHistoryId && (
                    <div className="history-code-wrap">
                      <div className="section-header small">Selected State JSON</div>
                      <pre className="history-code">{selectedHistoryCode || 'State payload unavailable.'}</pre>
                    </div>
                  )}
                </section>
              )}

              {activeTab === 'diff' && (
                <section className="detail-section glass-panel">
                  <div className="section-header">Compare State Versions</div>

                  <div className="diff-controls">
                    <label>
                      From
                      <select value={diffFromId} onChange={(e) => setDiffFromId(e.target.value)}>
                        <option value="">Select version</option>
                        {stateHistory.map((sv) => (
                          <option key={`from-${sv.ID}`} value={sv.ID}>v{sv.Serial} ({new Date(sv.CreatedAt).toLocaleTimeString()})</option>
                        ))}
                      </select>
                    </label>
                    <label>
                      To
                      <select value={diffToId} onChange={(e) => setDiffToId(e.target.value)}>
                        <option value="">Select version</option>
                        {stateHistory.map((sv) => (
                          <option key={`to-${sv.ID}`} value={sv.ID}>v{sv.Serial} ({new Date(sv.CreatedAt).toLocaleTimeString()})</option>
                        ))}
                      </select>
                    </label>
                  </div>

                  {!stateDiff ? (
                    <p className="hint">Select two versions to compute a diff.</p>
                  ) : (
                    <>
                      <div className="diff-summary">
                        <div className="summary-pill add">Added {stateDiff.summary?.added || 0}</div>
                        <div className="summary-pill change">Changed {stateDiff.summary?.changed || 0}</div>
                        <div className="summary-pill remove">Removed {stateDiff.summary?.removed || 0}</div>
                      </div>

                      <div className="diff-lists">
                        <div>
                          <h4>Added</h4>
                          <ul>{(stateDiff.added || []).map((item) => <li key={`a-${item}`}>{item}</li>)}</ul>
                        </div>
                        <div>
                          <h4>Changed</h4>
                          <ul>{(stateDiff.changed || []).map((item) => <li key={`c-${item}`}>{item}</li>)}</ul>
                        </div>
                        <div>
                          <h4>Removed</h4>
                          <ul>{(stateDiff.removed || []).map((item) => <li key={`r-${item}`}>{item}</li>)}</ul>
                        </div>
                      </div>

                      <h4>Git-style JSON Diff</h4>
                      <div className="git-diff">
                        {gitLikeDiffBlocks.map((line) => (
                          <div key={line.key} className={`git-diff-line ${line.type}`}>
                            <span className="prefix">{line.type === 'added' ? '+' : line.type === 'removed' ? '-' : ' '}</span>
                            <span>{line.text}</span>
                          </div>
                        ))}
                        {gitLikeDiffBlocks.length === 0 && <pre className="diff-raw">{diffRawPreview}</pre>}
                      </div>
                    </>
                  )}
                </section>
              )}

              {activeTab === 'runs' && (
                <section className="detail-section glass-panel">
                  <div className="section-header">CLI Runs</div>

                  {runs.length === 0 ? (
                    <div className="empty-guide">
                      <h3>No runs yet</h3>
                      <p>Push Terraform command output with the tfvision CLI to populate this page.</p>
                    </div>
                  ) : (
                    <div className="runs-layout">
                      <div className="timeline runs-timeline">
                        {runs.map((run) => (
                          <button
                            key={run.id}
                            className={`timeline-item ${selectedRunId === run.id ? 'active' : ''}`}
                            onClick={() => setSelectedRunId(run.id)}
                          >
                            <div className="timeline-marker"></div>
                            <div className="timeline-content">
                              <span className="sv-serial">terraform {run.command}</span>
                              <span className={`run-status-chip ${run.status}`}>{run.status}</span>
                              <span className="sv-date">{new Date(run.created_at).toLocaleString()}</span>
                              <span className="sv-lineage">{run.message || 'No message'}</span>
                            </div>
                          </button>
                        ))}
                      </div>

                      <aside className="run-details-panel glass-panel">
                        {selectedRun ? (
                          <>
                            <h4>terraform {selectedRun.command}</h4>
                            <div className="run-meta-grid">
                              <span>Status</span><strong>{selectedRun.status}</strong>
                              <span>Message</span><strong>{selectedRun.message || 'None'}</strong>
                              <span>Created</span><strong>{selectedRun.created_at ? new Date(selectedRun.created_at).toLocaleString() : 'n/a'}</strong>
                              <span>Completed</span><strong>{selectedRun.completed_at ? new Date(selectedRun.completed_at).toLocaleString() : 'n/a'}</strong>
                              <span>State Version</span><strong>{selectedRun.state_version_id || 'n/a'}</strong>
                            </div>
                            <h5>Logs</h5>
                            <div className="run-log" role="log" aria-label="Terraform run log output">
                              {formattedRunLog.length === 0 ? (
                                <div className="run-log-line blank">No logs uploaded.</div>
                              ) : (
                                formattedRunLog.map((line) => (
                                  <div key={line.key} className={`run-log-line ${line.tone}`}>
                                    {line.text === '' ? '\u00A0' : line.text}
                                  </div>
                                ))
                              )}
                            </div>
                          </>
                        ) : (
                          <p className="hint">Select a run to inspect logs and metadata.</p>
                        )}
                      </aside>
                    </div>
                  )}
                </section>
              )}

              {activeTab === 'resources' && (
                <section className="detail-section glass-panel">
                  <div className="section-header">Resource Canvas</div>

                  <div className="resource-toolbar">
                    <input
                      value={resourceFilter}
                      onChange={(e) => setResourceFilter(e.target.value)}
                      placeholder="Filter by address, type, module, provider"
                    />
                    <span>{filteredResources.length} nodes</span>
                  </div>

                  {filteredResources.length === 0 ? (
                    <div className="empty-guide">
                      <h3>No resources to show</h3>
                      <p>Apply infrastructure and return to this view to explore the graph.</p>
                    </div>
                  ) : (
                    <div className="canvas-wrap">
                      <div className="canvas-stage" style={{ width: `${layout.width}px`, height: `${layout.height}px` }}>
                        <svg className="canvas-edges" width={layout.width} height={layout.height}>
                          {layout.edges.map((edge, index) => (
                            <line
                              key={`${edge.from}-${edge.to}-${index}`}
                              x1={edge.x1}
                              y1={edge.y1}
                              x2={edge.x2}
                              y2={edge.y2}
                            />
                          ))}
                        </svg>

                        {layout.nodes.map((node) => (
                          <button
                            key={node.key}
                            className={`resource-node ${selectedResourceKey === node.key ? 'active' : ''}`}
                            style={{ left: `${node.x}px`, top: `${node.y}px` }}
                            onClick={() => setSelectedResourceKey(node.key)}
                          >
                            <div className="resource-node-type">{node.module_path && node.module_path !== 'root' ? 'MODULE RESOURCE' : 'RESOURCE'}</div>
                            <div className="resource-node-name">{node.name}</div>
                            <div className="resource-node-module">{node.module_path || 'root module'}</div>
                            <div className="resource-node-address">{node.address}</div>
                          </button>
                        ))}
                      </div>

                      <aside className="resource-details glass-panel">
                        {selectedResource ? (
                          <>
                            <h4>{selectedResource.address}</h4>
                            <div className="detail-grid">
                              <span>ID</span><strong>{selectedResource.id || 'n/a'}</strong>
                              <span>Type</span><strong>{selectedResource.type}</strong>
                              <span>Name</span><strong>{selectedResource.name}</strong>
                              <span>Provider</span><strong>{selectedResource.provider}</strong>
                              <span>Source</span><strong>{selectedResource.provider_source || 'unknown'}</strong>
                              <span>Version</span><strong>{selectedResource.provider_version || 'unknown'}</strong>
                              <span>Module</span><strong>{selectedResource.module_path || 'root'}</strong>
                              <span>Status</span><strong>{selectedResource.status || 'managed'}</strong>
                              <span>Depends On</span><strong>{(selectedResource.depends_on || []).join(', ') || 'None'}</strong>
                            </div>
                            <h5>Attributes</h5>
                            <pre className="attr-json">{JSON.stringify(selectedResource.attributes || {}, null, 2)}</pre>
                          </>
                        ) : (
                          <p className="hint">Click any node in the canvas to inspect details.</p>
                        )}
                      </aside>
                    </div>
                  )}
                </section>
              )}
            </div>
          )}
        </div>
      </main>
    </div>
  );
}

export default App;
