export type Preferences = { area: string; budget: string; beds: string };
export type SearchQuery = { area: string; max_rent_cents?: number; min_beds?: number; latitude?: number; longitude?: number };
export const preferenceKey = "haven:search-preferences:v1";
export const emptyPreferences: Preferences = { area: "", budget: "", beds: "" };
const validArea = (area: string) => /^\d{5}(?:-\d{4})?$/.test(area.trim()) || /^.{2,100}(?:,\s*|\s+)[a-z]{2}$/i.test(area.trim());
export const isAmherstSearch = (area: string) => /^(?:amherst(?:,\s*|\s+)ma|01002(?:-\d{4})?)$/i.test(area.trim());

export function readPreferences(value: unknown): Preferences {
  if (!value || typeof value !== "object") return { ...emptyPreferences };
  const item = value as Record<string, unknown>;
  return {
    area: typeof item.area === "string" ? item.area.slice(0, 120).trim() : "",
    budget: typeof item.budget === "string" ? item.budget.slice(0, 20) : "",
    beds: typeof item.beds === "string" && /^(|[0-9]|10)$/.test(item.beds) ? item.beds : "",
  };
}
export function searchQuery(p: Preferences, nearby?: { latitude: number; longitude: number }): SearchQuery {
  const area = p.area.trim();
  if (!nearby && !validArea(area)) throw new Error("Enter a ZIP code or city and state, such as 01002, Amherst MA, or Amherst, MA.");
  const query: SearchQuery = { area: nearby ? "" : area };
  if (p.budget.trim()) {
    const budget = Number(p.budget);
    if (!Number.isFinite(budget) || budget <= 0 || budget > 1_000_000) throw new Error("Enter a monthly budget greater than zero.");
    query.max_rent_cents = Math.round(budget * 100);
  }
  if (p.beds !== "") {
    const beds = Number(p.beds);
    if (!Number.isInteger(beds) || beds < 0 || beds > 10) throw new Error("Choose a valid bedroom preference.");
    query.min_beds = beds;
  }
  if (nearby) {
    if (!Number.isFinite(nearby.latitude) || !Number.isFinite(nearby.longitude) || Math.abs(nearby.latitude) > 90 || Math.abs(nearby.longitude) > 180) throw new Error("We couldn't determine your area. Enter a city or ZIP instead.");
    query.latitude = Math.round(nearby.latitude * 100) / 100;
    query.longitude = Math.round(nearby.longitude * 100) / 100;
  }
  return query;
}
