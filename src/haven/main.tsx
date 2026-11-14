import { StrictMode, useEffect, useRef, useState, type FormEvent } from "react";
import { createRoot } from "react-dom/client";
import { RentalCostPlanner } from "../app/components/RentalCostPlanner";
import { emptyPreferences, preferenceKey, readPreferences, searchQuery, type Preferences, type SearchQuery } from "./preferences";
import "../styles/index.css";
import "./styles.css";

type Rental = {
  id: string; address: string; city: string; state: string; rent_cents: number;
  beds: number | null; baths: number | null; last_seen: string; listed_at: string;
  source: string; agent_website?: string; reasons: string[];
  history: { date: string; rent_cents: number; event: string }[];
};
type Results = { listings: Rental[]; retrieved_at: string; source: string; has_more: boolean };
const defaultApi = import.meta.env.DEV ? "http://127.0.0.1:8081" : "";
const api = (import.meta.env.VITE_HAVEN_API_URL || defaultApi).replace(/\/$/, "");
const savedKey = "haven:saved-rentals:v1";
const money = (cents: number) => new Intl.NumberFormat("en-US", { style: "currency", currency: "USD", maximumFractionDigits: 2 }).format(cents / 100);
function date(value: string) { const d = new Date(value.length === 10 ? `${value}T12:00:00Z` : value); return Number.isNaN(d.getTime()) ? "Date not supplied" : d.toLocaleDateString("en-US", { month: "short", day: "numeric", year: "numeric" }); }
function storedPreferences(): Preferences { try { return readPreferences(JSON.parse(localStorage.getItem(preferenceKey) ?? "null")); } catch { return { ...emptyPreferences }; } }
function storedRentals(): Rental[] {
  try {
    const value: unknown = JSON.parse(localStorage.getItem(savedKey) ?? "[]");
    if (!Array.isArray(value)) return [];
    return value.filter((r): r is Rental => Boolean(r && typeof r.id === "string" && typeof r.address === "string" && typeof r.city === "string" && typeof r.state === "string" && typeof r.last_seen === "string" && typeof r.listed_at === "string" && (r.beds === null || typeof r.beds === "number") && (r.baths === null || typeof r.baths === "number") && r.source === "RentCast" && typeof r.rent_cents === "number" && r.rent_cents > 0 && Array.isArray(r.history) && Array.isArray(r.reasons)));
  } catch { return []; }
}
function initialQuery(): SearchQuery | null { try { const p = storedPreferences(); return p.area ? searchQuery(p) : null; } catch { return null; } }

function Haven() {
  const [preferences, setPreferences] = useState<Preferences>(storedPreferences);
  const [query, setQuery] = useState<SearchQuery | null>(initialQuery);
  const [nearby, setNearby] = useState<{ latitude: number; longitude: number } | undefined>();
  const [result, setResult] = useState<Results | null>(null);
  const [saved, setSaved] = useState<Rental[]>(storedRentals);
  const [savedOnly, setSavedOnly] = useState(false);
  const [selected, setSelected] = useState<Rental | null>(null);
  const [liveAvailable, setLiveAvailable] = useState<boolean | null>(null);
  const [loading, setLoading] = useState(false);
  const [locating, setLocating] = useState(false);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const [refresh, setRefresh] = useState(0);
  const locationRequest = useRef(0);

  useEffect(() => {
    // Remove obsolete IDs from the retired seeded preview. No runtime seed exists.
    try { localStorage.removeItem("haven:saved-properties:v1"); } catch { /* browser storage is optional */ }
    const controller = new AbortController();
    fetch(`${api}/api/v1/meta`, { signal: controller.signal })
      .then(async response => { if (!response.ok) throw Error(); const meta = await response.json(); setLiveAvailable(meta.live_search_available === true); })
      .catch(() => { if (!controller.signal.aborted) setLiveAvailable(false); });
    return () => { controller.abort(); locationRequest.current++; };
  }, [refresh]);

  useEffect(() => {
    if (!query || liveAvailable !== true) return;
    const controller = new AbortController(); setLoading(true); setError(""); setResult(null); setSelected(null);
    fetch(`${api}/api/v1/rentals/search`, { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(query), signal: controller.signal })
      .then(async response => {
        const contentType = response.headers.get("content-type") ?? "";
        if (!contentType.toLowerCase().includes("json")) {
          setLiveAvailable(false); setError(""); setResult(null); return;
        }
        let data: Results & { code?: string };
        try { data = await response.json(); }
        catch { setLiveAvailable(false); setError(""); setResult(null); return; }
        if (!response.ok) {
          if (data.code === "not_connected") { setLiveAvailable(false); setError(""); return; }
          throw Error(data.code === "invalid_search" ? "Enter a ZIP code or city and state, and check your preferences." : "We couldn't load rentals right now. Please try again shortly.");
        }
        if (!Array.isArray(data.listings)) throw Error("We couldn't load rentals right now.");
        setLiveAvailable(true); setResult(data);
      })
      .catch(e => { if (!controller.signal.aborted) setError(e instanceof Error ? e.message : "Unable to search right now."); })
      .finally(() => { if (!controller.signal.aborted) setLoading(false); });
    return () => controller.abort();
  }, [query, refresh, liveAvailable]);

  function submit(event: FormEvent) {
    event.preventDefault(); setNotice("");
    if (liveAvailable !== true) { setError(""); return; }
    try {
      const next = searchQuery(preferences, nearby);
      try { localStorage.setItem(preferenceKey, JSON.stringify({ ...preferences, area: nearby ? "" : preferences.area.trim() })); } catch { setNotice("Your search will work, but this browser couldn't remember your preferences."); }
      setSavedOnly(false); setQuery(next); setError("");
    } catch (e) { setError(e instanceof Error ? e.message : "Check your search preferences."); }
  }
  function useLocation() {
    if (!navigator.geolocation) { setError("Location isn't supported in this browser. Enter a city or ZIP instead."); return; }
    const request = ++locationRequest.current; setLocating(true); setError("");
    navigator.geolocation.getCurrentPosition(position => {
      if (request !== locationRequest.current) return;
      try {
        const location = { latitude: position.coords.latitude, longitude: position.coords.longitude };
        const next = searchQuery(preferences, location); setNearby({ latitude: next.latitude!, longitude: next.longitude! }); setPreferences(p => ({ ...p, area: "" })); setQuery(next); setSavedOnly(false);
      } catch (e) { setError(e instanceof Error ? e.message : "Enter your area instead."); }
      setLocating(false);
    }, () => { if (request === locationRequest.current) { setLocating(false); setError("We couldn't access your location. Enter a city or ZIP to continue."); } }, { enableHighAccuracy: false, maximumAge: 300000, timeout: 10000 });
  }
  function clearSearch() {
    locationRequest.current++; setLocating(false); setNearby(undefined); setPreferences({ ...emptyPreferences });
    setQuery(null); setResult(null); setSelected(null); setLoading(false); setError(""); setNotice(""); setSavedOnly(false);
    try { localStorage.removeItem(preferenceKey); } catch { setNotice("Your browser couldn't clear remembered preferences."); }
  }
  function toggleSaved(rental: Rental) {
    const next = saved.some(item => item.id === rental.id) ? saved.filter(item => item.id !== rental.id) : [...saved, rental];
    try { localStorage.setItem(savedKey, JSON.stringify(next)); setSaved(next); setNotice(""); } catch { setNotice("Your browser couldn't save this rental. Check its storage settings."); }
  }
  const shown = savedOnly ? saved : result?.listings ?? [];
  const areaLabel = query?.latitude !== undefined ? "near you" : query?.area || "your area";

  return <div className="haven-app">
    <header className="haven-header"><a className="haven-brand" href="/haven.html"><span className="haven-mark">h.</span>Haven</a><button className="haven-secondary" onClick={() => { setSavedOnly(v => !v); setSelected(null); }}>{savedOnly ? "Back to search" : `Saved (${saved.length})`}</button></header>
    <main className="haven-main haven-customer">
      <section className="haven-intro"><div><p className="haven-eyebrow">YOUR NEXT HOME, WITH FEWER SURPRISES</p><h1>Find a rental that fits your life.</h1><p>Real listings for your area and budget. Clear costs before you commit.</p></div></section>
      <form className="haven-search-form" onSubmit={submit}>
        <label className="haven-area-field">Where do you want to live?<input aria-describedby="location-note" value={nearby ? "Near my location" : preferences.area} maxLength={120} placeholder="City, state or ZIP" onChange={e => { locationRequest.current++; setLocating(false); setNearby(undefined); setPreferences(p => ({ ...p, area: e.target.value })); }}/></label>
        <label>Max rent / month<input type="number" min="1" max="1000000" step="0.01" placeholder="Any budget" value={preferences.budget} onChange={e => setPreferences(p => ({ ...p, budget: e.target.value }))}/></label>
        <label>Bedrooms<select value={preferences.beds} onChange={e => setPreferences(p => ({ ...p, beds: e.target.value }))}><option value="">Any</option><option value="0">Studio</option>{[1,2,3,4,5].map(n => <option key={n} value={String(n)}>{n}+</option>)}</select></label>
        <button className="haven-primary" type="submit" disabled={loading || locating || liveAvailable !== true}>{loading ? "Searching…" : liveAvailable === null ? "Checking search…" : liveAvailable ? "Find rentals" : "Search unavailable"}</button>
        <div className="haven-location-row"><button className="haven-location-button" type="button" disabled={locating || liveAvailable !== true} onClick={useLocation}>{locating ? "Finding your area…" : "⌖ Use my location"}</button><span id="location-note">Optional. Nearby searches share approximate coordinates with RentCast, with your permission.</span>{(preferences.area || preferences.budget || preferences.beds || nearby) && <button type="button" className="haven-location-button" onClick={clearSearch}>Clear search</button>}</div>
      </form>
      {notice && <p className="haven-customer-note" role="status">{notice}</p>}
      {error && <div className="haven-error" role="alert">{error}<button onClick={() => setRefresh(n => n + 1)}>Try again</button></div>}
      <section aria-live="polite" className="haven-results">
        <div className="haven-section-heading"><h2>{savedOnly ? "Your saved rentals" : result ? `Rentals ${areaLabel}` : "Made for your search"}</h2>{result && !savedOnly && <span>{shown.length} results · sorted by listed rent</span>}</div>
        {loading && !savedOnly && <div className="haven-panel haven-empty">Finding rentals that match your preferences…</div>}
        {!loading && !savedOnly && !result && !error && <div className="haven-welcome"><span className="haven-welcome-icon">⌂</span><h2>{liveAvailable === false ? "Live rentals aren't available yet." : "Start with the area you love."}</h2><p>{liveAvailable === false ? "Check back soon. No listings are shown until real rental data is available." : "Choose your location. Add a budget if you have one. We'll take it from there."}</p></div>}
        {((result && !loading) || savedOnly) && shown.length === 0 && <div className="haven-welcome"><h2>{savedOnly ? "Keep your favorites here." : "No rentals found for this search."}</h2><p>{savedOnly ? "Save a rental to come back to its details and costs." : "Try a nearby area or adjust your budget or bedrooms. We won't fill the gaps with unrelated listings."}</p></div>}
        <div className="haven-rental-grid">{shown.map(rental => <article key={rental.id} className="haven-rental-card"><div className="haven-rental-top"><span>{rental.city}, {rental.state}</span><button aria-label={`${saved.some(r => r.id === rental.id) ? "Unsave" : "Save"} ${rental.address}`} aria-pressed={saved.some(r => r.id === rental.id)} onClick={() => toggleSaved(rental)}>{saved.some(r => r.id === rental.id) ? "★" : "☆"}</button></div><h3>{rental.address}</h3><p className="haven-rental-price">{money(rental.rent_cents)}<small> / month</small></p><p className="haven-rental-size">{rental.beds === null ? "Beds not supplied" : rental.beds === 0 ? "Studio" : `${rental.beds} beds`} · {rental.baths === null ? "Baths not supplied" : `${rental.baths} baths`}</p>{!savedOnly && <div className="haven-reasons">{rental.reasons.map(reason => <span key={reason}>{reason}</span>)}</div>}<p className="haven-customer-note">{savedOnly ? "Saved record" : "Source-reported active"} · Last seen {date(rental.last_seen)}</p><button className="haven-secondary" aria-expanded={selected?.id === rental.id} onClick={() => setSelected(current => current?.id === rental.id ? null : rental)}>{selected?.id === rental.id ? "Close details" : "History & full costs"}</button></article>)}</div>
        {result?.has_more && !savedOnly && <p className="haven-customer-note">Showing the first 50 source results. Narrow your area or preferences to see a more focused selection.</p>}
        {result && !savedOnly && <p className="haven-customer-note">Data from RentCast · Retrieved {date(result.retrieved_at)}. Listing availability can change; confirm with the property contact. Listed rent may exclude fees.</p>}
      </section>
      {selected && <section className="haven-selected"><div className="haven-section-heading"><h2>{selected.address}</h2><button className="haven-location-button" onClick={() => setSelected(null)}>Close</button></div><div className="haven-panel"><h3>Rental history</h3>{selected.history.length ? <div className="haven-timeline">{selected.history.map((item,index) => <article key={`${item.date}-${index}`}><div className="haven-timeline-dot"/><strong>{money(item.rent_cents)} / month</strong><p>{date(item.date)} · {item.event || "Rental listing"}</p></article>)}</div> : <p>No prior rental history was supplied for this listing.</p>}<p className="haven-customer-note">Source: {selected.source}. These are provider-reported records, not independent verification.</p>{selected.agent_website && /^https:\/\//.test(selected.agent_website) && <a href={selected.agent_website} target="_blank" rel="noopener noreferrer">Visit the listing agent's website ↗</a>}</div><RentalCostPlanner key={selected.id} rent={selected.rent_cents / 100}/></section>}
      <footer className="haven-footer"><strong>Haven</strong><span>Your area. Your budget. A clearer next step.</span></footer>
    </main>
  </div>;
}
createRoot(document.getElementById("haven-root")!).render(<StrictMode><Haven/></StrictMode>);
