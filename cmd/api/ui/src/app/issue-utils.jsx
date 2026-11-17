/* ════════════════ ISSUE MODAL ════════════════ */
/* Builds the recurring-issue warning sentence in the active language. */
function duplicateAlertMessage(lang, count, location, windowHours) {
  if (lang === 'pt') {
    return `Este tipo de avaria já ocorreu ${count} ${count===1?'vez':'vezes'} em ${location} nas últimas ${windowHours}h.`;
  }
  return `This issue type has happened ${count} ${count===1?'time':'times'} at ${location} in the last ${windowHours}h.`;
}

/* Computes whole minutes between two "HH:MM" times, treating an end time
   earlier than the start time as crossing midnight. Returns null when the
   inputs are missing or the duration works out to zero. */
function computeDurationMinutes(startTime, endTime) {
  if (!startTime || !endTime) return null;
  const hasDate = startTime.includes('T') || endTime.includes('T');
  if (hasDate) {
    if (!startTime.includes('T') || !endTime.includes('T')) return null;
    const s = new Date(startTime), e = new Date(endTime);
    if (isNaN(s) || isNaN(e)) return null;
    const diff = Math.round((e - s) / 60000);
    return diff > 0 ? diff : null;
  }
  const [sh, sm] = startTime.split(':').map(Number);
  const [eh, em] = endTime.split(':').map(Number);
  if ([sh,sm,eh,em].some(n => Number.isNaN(n))) return null;
  let diff = (eh*60+em) - (sh*60+sm);
  if (diff < 0) diff += 24*60; // crossed midnight, legacy clock-only
  return diff > 0 ? diff : null;
}

