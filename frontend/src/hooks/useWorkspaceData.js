import { useEffect } from 'react';
import * as api from '../api';

/**
 * useWorkspaceData fires concurrent data fetches whenever the selected
 * workspace changes and reports the results via onData.
 *
 * @param {object|null} selectedWorkspace - The currently selected workspace (must have an .ID field).
 * @param {function}    onData            - Called with { history, resources, runs, stateSummary }.
 * @param {function}    onError           - Called with an Error if any fetch fails.
 */
export function useWorkspaceData(selectedWorkspace, onData, onError) {
  useEffect(() => {
    if (!selectedWorkspace) return;

    let cancelled = false;

    const load = async () => {
      try {
        const [history, resources, stateSummary, runs] = await Promise.all([
          api.fetchStateVersions(selectedWorkspace.ID),
          api.fetchResources(selectedWorkspace.ID),
          api.fetchStateSummary(selectedWorkspace.ID),
          api.fetchRuns(selectedWorkspace.ID),
        ]);
        if (!cancelled) {
          onData({ history, resources, runs, stateSummary });
        }
      } catch (err) {
        if (!cancelled) {
          onError(err);
        }
      }
    };

    load();

    return () => {
      cancelled = true;
    };
  }, [selectedWorkspace]); // eslint-disable-line react-hooks/exhaustive-deps
}

/**
 * useHistoryDetail fetches the full detail for a selected state version.
 *
 * @param {object|null} selectedWorkspace
 * @param {string}      selectedHistoryId
 * @param {function}    onDetail - Called with the detail object or null.
 */
export function useHistoryDetail(selectedWorkspace, selectedHistoryId, onDetail) {
  useEffect(() => {
    let cancelled = false;

    const load = async () => {
      if (!selectedWorkspace || !selectedHistoryId) {
        onDetail(null);
        return;
      }
      const detail = await api.fetchStateVersionSummary(selectedWorkspace.ID, selectedHistoryId);
      if (!cancelled) onDetail(detail);
    };

    load();
    return () => { cancelled = true; };
  }, [selectedWorkspace, selectedHistoryId]); // eslint-disable-line react-hooks/exhaustive-deps
}

/**
 * useStateDiff fetches the diff between two state versions.
 *
 * @param {object|null} selectedWorkspace
 * @param {string}      diffFromId
 * @param {string}      diffToId
 * @param {function}    onDiff - Called with the diff object or null.
 */
export function useStateDiff(selectedWorkspace, diffFromId, diffToId, onDiff) {
  useEffect(() => {
    let cancelled = false;

    const load = async () => {
      if (!selectedWorkspace || !diffFromId || !diffToId) {
        onDiff(null);
        return;
      }
      const diff = await api.fetchStateDiff(selectedWorkspace.ID, diffFromId, diffToId);
      if (!cancelled) onDiff(diff);
    };

    load();
    return () => { cancelled = true; };
  }, [selectedWorkspace, diffFromId, diffToId]); // eslint-disable-line react-hooks/exhaustive-deps
}

/**
 * useRunDetail fetches the full log body and metadata for a selected run.
 *
 * @param {string}   selectedRunId
 * @param {function} onDetail - Called with the run detail object or null.
 */
export function useRunDetail(selectedRunId, onDetail) {
  useEffect(() => {
    let cancelled = false;

    const load = async () => {
      if (!selectedRunId) {
        onDetail(null);
        return;
      }
      const detail = await api.fetchRunDetail(selectedRunId);
      if (!cancelled) onDetail(detail);
    };

    load();
    return () => { cancelled = true; };
  }, [selectedRunId]); // eslint-disable-line react-hooks/exhaustive-deps
}
