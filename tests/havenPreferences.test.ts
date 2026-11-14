import { test } from "node:test";
import assert from "node:assert/strict";
import { readPreferences, searchQuery } from "../src/haven/preferences.ts";

test("a first visit has no invented location or budget", () => {
  assert.deepEqual(readPreferences(null), { area: "", budget: "", beds: "" });
  assert.throws(() => searchQuery(readPreferences(null)), /ZIP/);
});
test("returning preferences retain the actual area and optional budget", () => {
  const p = readPreferences({ area: " 01002 ", budget: "1800.50", beds: "2" });
  assert.deepEqual(searchQuery(p), { area: "01002", max_rent_cents: 180050, min_beds: 2 });
  assert.deepEqual(searchQuery({ area: "Amherst, MA", budget: "", beds: "" }), { area: "Amherst, MA" });
});
test("manual locations accept ZIP+4 and common city/state typing", () => {
  for (const area of ["01002-1234", "amherst ma", "Amherst, ma", "New York, NY"]) {
    assert.equal(searchQuery({ area, budget: "", beds: "" }).area, area);
  }
});
test("nearby search uses coarse coordinates and preserves studio as zero", () => {
  assert.deepEqual(searchQuery({ area: "", budget: "", beds: "0" }, { latitude: 42.37322, longitude: -72.51993 }), { area: "", min_beds: 0, latitude: 42.37, longitude: -72.52 });
});
test("invalid budgets and locations cannot silently broaden a search", () => {
  for (const budget of ["-1", "NaN", "Infinity", "0"]) assert.throws(() => searchQuery({ area: "01002", budget, beds: "" }));
  assert.throws(() => searchQuery({ area: "", budget: "", beds: "" }, { latitude: 200, longitude: 0 }));
});
