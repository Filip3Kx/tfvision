const ANSI_ESCAPE_PATTERN = /\u001b\[[0-9;]*m/g;
const BRACKET_COLOR_CODE_PATTERN = /\[(?:\d{1,3}(?:;\d{1,3})*)m/g;

/**
 * Strips ANSI colour codes and carriage returns from a single log line.
 */
export function normalizeLogLine(line) {
  return line
    .replace(ANSI_ESCAPE_PATTERN, '')
    .replace(BRACKET_COLOR_CODE_PATTERN, '')
    .replace(/\r/g, '');
}

/**
 * Returns a semantic tone class for a cleaned log line.
 * Used to apply colour-coded styling in the Runs log viewer.
 */
export function classifyLogLine(line) {
  const trimmed = line.trim();
  if (trimmed === '') return 'blank';
  if (/^error:|\berror\b|^\u2577|^\u2575/i.test(trimmed)) return 'error';
  if (/^warning:|\bwarning\b/i.test(trimmed)) return 'warning';
  if (/^apply complete!|^plan:\s+\d+ to add, \d+ to change, \d+ to destroy\.?$/i.test(trimmed)) return 'summary';
  if (/\b(will be created|creation complete|created)\b/i.test(trimmed) || /^\+/.test(trimmed)) return 'added';
  if (/\b(will be updated|updated in-place|modifying)\b/i.test(trimmed) || /^~/.test(trimmed)) return 'changed';
  if (/\b(will be destroyed|destroy complete|destroyed)\b/i.test(trimmed) || /^-/.test(trimmed)) return 'removed';
  if (
    /^terraform will perform the following actions:$/i.test(trimmed) ||
    /^terraform used the selected providers/i.test(trimmed)
  )
    return 'heading';
  return 'context';
}

/**
 * Converts a raw log string into an array of annotated line objects ready for
 * rendering.  Each object has { key, text, tone }.
 */
export function formatRunLog(rawLog) {
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
