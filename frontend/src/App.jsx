import React, { useEffect, useMemo, useReducer } from 'react';
import { diffLines } from 'diff';
import './App.css';
import * as api from './api';
import { computeNodeLayout } from './utils/resourceLayout';
import { formatRunLog } from './utils/logFormatter';
import { useWorkspaceData, useHistoryDetail, useStateDiff, useRunDetail } from './hooks/useWorkspaceData';

// ---------------------------------------------------------------------------
// Utility helpers
// ---------------------------------------------------------------------------

function displayName(obj) {
  return obj?.Name || obj?.name || obj?.ID || obj?.id || 'unnamed';
}

function resourceKey(resource) {
  return resource?.id || resource?.address || `${resource?.type || 'resource'}.${resource?.name || 'unknown'}`;
}

// ---------------------------------------------------------------------------
// Reducer
// ---------------------------------------------------------------------------

const initialState = {
  organizations: [],
  selectedOrg: null,
  workspaces: [],
  selectedWorkspace: null,
  activeTab: 'history',
  stateHistory: [],
  resources: [],
  runs: [],
  stateSummary: null,
  selectedHistoryId: '',
  selectedHistoryDetail: null,
  selectedRunId: '',
  selectedRunDetail: null,
  diffFromId: '',
  diffToId: '',
  stateDiff: null,
  resourceFilter: '',
  selectedResourceKey: '',
  loading: true,
};

function appReducer(state, action) {
  switch (action.type) {
    case 'SET_LOADING':
      return { ...state, loading: action.value };
    case 'SET_ORGANIZATIONS':
      return { ...state, organizations: action.orgs };
    case 'SELECT_ORG':
      return {
        ...state,
        selectedOrg: action.org,
        selectedWorkspace: null,
        workspaces: [],
        stateHistory: [],
        resources: [],
        runs: [],
        stateSummary: null,
        selectedHistoryId: '',
        selectedHistoryDetail: null,
        selectedRunId: '',
        selectedRunDetail: null,
        stateDiff: null,
        resourceFilter: '',
        selectedResourceKey: '',
      };
    case 'SET_WORKSPACES':
      return { ...state, workspaces: action.workspaces };
    case 'SELECT_WORKSPACE':
      return {
        ...state,
        selectedWorkspace: action.workspace,
        activeTab: 'history',
        resourceFilter: '',
        selectedResourceKey: '',
        selectedHistoryId: '',
        selectedHistoryDetail: null,
        selectedRunId: '',
        selectedRunDetail: null,
      };
    case 'CLEAR_WORKSPACE':
      return { ...state, selectedWorkspace: null };
    case 'SET_WORKSPACE_DATA': {
      const history = action.history;
      const runs = action.runs;
      return {
        ...state,
        stateHistory: history,
        resources: action.resources,
        runs,
        stateSummary: action.stateSummary,
        selectedHistoryId: history[0]?.ID || '',
        selectedRunId: runs[0]?.id || '',
        diffFromId: history.length >= 2 ? history[1].ID : (history[0]?.ID || ''),
        diffToId: history[0]?.ID || '',
      };
    }
    case 'SET_ACTIVE_TAB':
      return { ...state, activeTab: action.tab };
    case 'SELECT_HISTORY_ID':
      return { ...state, selectedHistoryId: action.id, selectedHistoryDetail: null };
    case 'SET_HISTORY_DETAIL':
      return { ...state, selectedHistoryDetail: action.detail };
    case 'SELECT_RUN_ID':
      return { ...state, selectedRunId: action.id, selectedRunDetail: null };
    case 'SET_RUN_DETAIL':
      return { ...state, selectedRunDetail: action.detail };
    case 'SET_DIFF_FROM':
      return { ...state, diffFromId: action.id, stateDiff: null };
    case 'SET_DIFF_TO':
      return { ...state, diffToId: action.id, stateDiff: null };
    case 'SET_STATE_DIFF':
      return { ...state, stateDiff: action.diff };
    case 'SET_RESOURCE_FILTER':
      return { ...state, resourceFilter: action.filter };
    case 'SELECT_RESOURCE_KEY':
      return { ...state, selectedResourceKey: action.key };
    default:
      return state;
  }
}

// ---------------------------------------------------------------------------
// App component
// ---------------------------------------------------------------------------

function App() {
  const [state, dispatch] = useReducer(appReducer, initialState);

  const {
    organizations,
    selectedOrg,
    workspaces,
    selectedWorkspace,
    activeTab,
    stateHistory,
    resources,
    runs,
    stateSummary,
    selectedHistoryId,
    selectedHistoryDetail,
    selectedRunId,
    selectedRunDetail,
    diffFromId,
    diffToId,
    stateDiff,
    resourceFilter,
    selectedResourceKey,
    loading,
  } = state;

  // ---------------------------------------------------------------------------
  // Data loading hooks
  // ---------------------------------------------------------------------------

  useWorkspaceData(
    selectedWorkspace,
    ({ history, resources: res, runs: r, stateSummary: s }) => {
      dispatch({ type: 'SET_WORKSPACE_DATA', history, resources: res, runs: r, stateSummary: s });
    },
    (err) => console.error('Failed to load workspace details', err),
  );

  useHistoryDetail(selectedWorkspace, selectedHistoryId, (detail) => {
    dispatch({ type: 'SET_HISTORY_DETAIL', detail });
  });

  useStateDiff(selectedWorkspace, diffFromId, diffToId, (diff) => {
    dispatch({ type: 'SET_STATE_DIFF', diff });
  });

  useRunDetail(selectedRunId, (detail) => {
    dispatch({ type: 'SET_RUN_DETAIL', detail });
  });

  // ---------------------------------------------------------------------------
  // Org / workspace actions
  // ---------------------------------------------------------------------------

  useEffect(() => {
    loadOrganizations();
  }, []);

  const loadOrganizations = async () => {
    try {
      dispatch({ type: 'SET_LOADING', value: true });
      const orgs = await api.fetchOrgs();
      dispatch({ type: 'SET_ORGANIZATIONS', orgs });
      if (orgs.length > 0) {
        await handleSelectOrg(orgs[0]);
      }
    } catch (err) {
      console.error('Failed to load organizations', err);
    } finally {
      dispatch({ type: 'SET_LOADING', value: false });
    }
  };

  const handleCreateOrg = async () => {
    const name = prompt('Organization name');
    if (!name) return;
    try {
      await api.createOrg(name);
      await loadOrganizations();
    } catch {
      alert('Failed to create organization');
    }
  };

  const handleSelectOrg = async (org) => {
    dispatch({ type: 'SELECT_ORG', org });
    try {
      const ws = await api.fetchWorkspaces(org.ID);
      dispatch({ type: 'SET_WORKSPACES', workspaces: ws });
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
      dispatch({ type: 'SET_WORKSPACES', workspaces: ws });
    } catch {
      alert('Failed to create workspace');
    }
  };

  const handleSelectWorkspace = (workspace) => {
    dispatch({ type: 'SELECT_WORKSPACE', workspace });
  };

  // ---------------------------------------------------------------------------
  // Memoised derived values
  // ---------------------------------------------------------------------------

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
    return resources.find((r) => resourceKey(r) === selectedResourceKey) || null;
  }, [resources, selectedResourceKey]);

  const selectedSummary = selectedHistoryDetail?.summary || stateSummary;

  const selectedRun = useMemo(() => {
    if (!selectedRunId) return null;
    return runs.find((r) => r.id === selectedRunId) || selectedRunDetail;
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

  const diffRawPreview = useMemo(() => {
    if (!stateDiff?.raw) return '';
    return JSON.stringify(stateDiff.raw, null, 2);
  }, [stateDiff]);

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

  // ---------------------------------------------------------------------------
  // Render
  // ---------------------------------------------------------------------------

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
                <button className="back-btn" onClick={() => dispatch({ type: 'CLEAR_WORKSPACE' })}>Back to Workspaces</button>
                <h2>{displayName(selectedWorkspace)}</h2>
              </div>

              <div className="tab-nav glass-panel">
                <button className={`tab-btn ${activeTab === 'history' ? 'active' : ''}`} onClick={() => dispatch({ type: 'SET_ACTIVE_TAB', tab: 'history' })}>State History</button>
                <button className={`tab-btn ${activeTab === 'diff' ? 'active' : ''}`} onClick={() => dispatch({ type: 'SET_ACTIVE_TAB', tab: 'diff' })}>State Diff</button>
                <button className={`tab-btn ${activeTab === 'runs' ? 'active' : ''}`} onClick={() => dispatch({ type: 'SET_ACTIVE_TAB', tab: 'runs' })}>Runs</button>
                <button className={`tab-btn ${activeTab === 'resources' ? 'active' : ''}`} onClick={() => dispatch({ type: 'SET_ACTIVE_TAB', tab: 'resources' })}>Resource Canvas</button>
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
                          onClick={() => dispatch({ type: 'SELECT_HISTORY_ID', id: sv.ID })}
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
                      <select value={diffFromId} onChange={(e) => dispatch({ type: 'SET_DIFF_FROM', id: e.target.value })}>
                        <option value="">Select version</option>
                        {stateHistory.map((sv) => (
                          <option key={`from-${sv.ID}`} value={sv.ID}>v{sv.Serial} ({new Date(sv.CreatedAt).toLocaleTimeString()})</option>
                        ))}
                      </select>
                    </label>
                    <label>
                      To
                      <select value={diffToId} onChange={(e) => dispatch({ type: 'SET_DIFF_TO', id: e.target.value })}>
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
                            onClick={() => dispatch({ type: 'SELECT_RUN_ID', id: run.id })}
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
                      onChange={(e) => dispatch({ type: 'SET_RESOURCE_FILTER', filter: e.target.value })}
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
                            onClick={() => dispatch({ type: 'SELECT_RESOURCE_KEY', key: node.key })}
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
