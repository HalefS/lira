/* ════════════════ PAGINATED LISTINGS ════════════════ */

/* How many rows a listing shows before the reader asks for more. Matches the
   server's own default, so a page that forgets to say anything still opens at
   20 rather than at whatever the endpoint would otherwise return. */
const PAGE_SIZE = 20;

/* Fetches a listing that grows a page at a time.

   buildPath(limit) returns the request path, so each caller keeps control of its
   own query parameters and the hook only owns how many rows have been asked for.

   filterKey is what the caller considers "these are different rows": change it
   and the limit drops back to a page. That is why it exists rather than asking
   the caller to remember to reset -- a filter that narrows to three rows should
   not open with two hundred already loaded behind them, and a reader who has
   loaded six pages and then searches should not have to click through five empty
   ones to find out their search matched nothing.

   The limit is a growing ceiling rather than a page number with an offset, so
   "load more" asks for everything already on screen plus one page more. Rows
   keep their order and their place, and there is no page number to fall out of
   step with the filters.

   rows is null until the first answer lands, which is how a page tells "still
   loading" from "loaded, and there is genuinely nothing".

   error is the failure message, or null. It exists because rows:null is the
   same value a failed request leaves behind, so a page gating on !rows cannot
   tell the two apart and renders its loading state forever. A page that wants
   to say something about a failure reads error; onError is only for the
   endpoints whose failure the page has to react to rather than just display.

   options:
     rowsKey   which key in the response holds the rows. Every endpoint names its
               own -- issues, units, swaps -- so this cannot be guessed at.
     map       applied to each row on the way in. The issue tables read a mapped
               shape and the rest read the API's own, so it is asked for rather
               than assumed.
     filterKey see above; changes reset the limit to a page.
     onError   for endpoints whose failure means something to the page, such as
               the LCU gate refusing the request. */
function usePagedList(buildPath, token, options = {}) {
  const {rowsKey = 'rows', map = null, filterKey = '', onError = null} = options;
  const [limit, setLimit] = useState(PAGE_SIZE);
  const [state, setState] = useState({rows: null, total: 0, capped: false, loading: true, res: null, error: null});

  // Reset during render rather than in an effect: an effect would build the path
  // once with the new filters and the old limit, fetch it, and then fetch again
  // after resetting -- two requests for one filter change, and whichever answered
  // last would win regardless of which was asked for more recently.
  const [prevKey, setPrevKey] = useState(filterKey);
  if (prevKey !== filterKey) {
    setPrevKey(filterKey);
    setLimit(PAGE_SIZE);
  }

  // buildPath is deliberately not a dependency: it is a fresh closure on every
  // render, and depending on it would refetch in a loop. filterKey stands in for
  // whatever the closure actually reads.
  const path = useMemo(() => buildPath(limit), [limit, filterKey]);

  // map and onError are held in refs so a caller passing an inline arrow does not
  // become an effect dependency and restart the fetch on every render.
  const mapRef = useRef(map);
  mapRef.current = map;
  const onErrorRef = useRef(onError);
  onErrorRef.current = onError;

  useEffect(() => {
    let cancelled = false;
    setState(s => ({...s, loading: true}));
    apiFetch(path, {token})
      .then(res => {
        if (cancelled) return;
        // Coerced to an array whatever came back: every caller maps over it, and
        // a missing or null key would otherwise read as "still loading" forever.
        const raw = res[rowsKey];
        setState({
          rows:   Array.isArray(raw) ? (mapRef.current ? raw.map(mapRef.current) : raw) : [],
          total:  Number(res.total) || 0,
          capped: !!res.limit_capped,
          loading: false,
          res,
          error: null,
        });
      })
      .catch(ex => {
        if (cancelled) return;
        // `error` is what separates "no answer yet" from "the answer was a
        // failure", which rows:null alone cannot: a caller gating on !rows drew
        // its loading state for a request that had already come back broken, and
        // a page that renders one forever is a page that looks like it is working.
        // It is a string rather than the exception so a caller can print it
        // directly, which is what the existing error banners do.
        setState({rows: null, total: 0, capped: false, loading: false, res: null, error: ex.message || 'Request failed'});
        if (onErrorRef.current) onErrorRef.current(ex);
      });
    return () => { cancelled = true; };
    // filterKey is a dependency even though it never appears in the path.
    //
    // A caller uses it to say "these are different rows" for reasons the query
    // string cannot express: an issue was just written and the page behind this
    // list has changed, or the daily answer was recorded. Bumping it recomputes
    // the path to the *identical* string, so on its own it would change nothing --
    // React would compare equal strings and correctly decline to run the effect,
    // and the list would sit there showing a snapshot from before the write.
  }, [path, token, rowsKey, filterKey]);

  return {
    // Spreads `res` through as well: an endpoint may answer with more than the
    // rows, and the LCU list carries the server's idea of today alongside them.
    ...state,
    // How many rows the server actually sent, which is not always how many are on
    // screen: a page that still narrows its own rows client-side shows fewer, and
    // the count here is the honest one for "you have loaded 20 of 340".
    fetched: state.rows ? state.rows.length : 0,
    limit,
    loadMore: () => setLimit(l => l + PAGE_SIZE),
  };
}

/* The footer under a paginated table: what is loaded out of what exists, and the
   button that asks for the next page.

   Renders nothing at all once everything is loaded -- an empty footer with a
   button that does nothing is worse than no footer. rows may be null while the
   first request is in flight, in which case this shows nothing rather than
   flashing "0 of 0".

   `shown` and `total` override the list's own figures. The daily report needs
   that: its two tables come from one response and share one limit, so neither
   table's count is the count of rows the server sent, and the honest pair is
   worked out from the per-mode totals the summary already carries. */
function LoadMore({ list, t, shown, total }) {
  const have = list && (list.rows || list.res);
  if (!have || list.loading) return null;
  const n = shown != null ? shown : list.fetched;
  const m = total  != null ? total  : list.total;
  if (!m || n >= m) return null;
  return (
    <div className="loadmore">
      <span className="loadmore-count">
        {t.showingOf.replace('{shown}', n).replace('{total}', m)}
      </span>
      <button className="btn btn-ghost" style={{padding:'6px 14px',fontSize:12.5}}
        onClick={list.loadMore} disabled={list.loading}>
        {list.loading ? '…' : t.loadMore}
      </button>
      {/* Only when the server refused to go past its own maximum, so a reader who
          reaches it is told why rather than left thinking they had seen
          everything. */}
      {list.capped && <span className="loadmore-note">{t.loadMoreCapped}</span>}
    </div>
  );
}

