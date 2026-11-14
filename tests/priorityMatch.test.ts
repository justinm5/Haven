import assert from "node:assert/strict";
import { test } from "node:test";
import { computeMatchPercent, computeMatchPercentMulti, parsePositivePriority } from "../src/app/lib/priorityMatch.ts";

const listing = { id: "example", price: 1800, beds: 2, baths: 1.5 };
test("all rentals within a maximum budget receive full budget credit", () => {
  for (const price of [0, 1000, 1800, 2000]) {
    assert.equal(computeMatchPercent({ ...listing, price }, "cost", 2000), 100);
  }
});
test("exceeding the budget reduces the match and is bounded at zero", () => {
  assert.equal(computeMatchPercent({ ...listing, price: 2500 }, "cost", 2000), 80);
  assert.equal(computeMatchPercent({ ...listing, price: 10000 }, "cost", 2000), 0);
});
test("fractional bathroom requirements survive parsing and scoring", () => {
  assert.equal(parsePositivePriority("1.5"), 1.5);
  assert.equal(computeMatchPercent({ ...listing, baths: 1 }, "baths", 1.5), 67);
});
test("blank, malformed, nonpositive and nonfinite priorities are rejected", () => {
  for (const value of [null, "", " ", "0", "-2", "2bed", "NaN", "Infinity"]) {
    assert.equal(parsePositivePriority(value), undefined);
  }
});
test("multiple priorities are averaged equally and invalid priorities are ignored", () => {
  assert.equal(computeMatchPercentMulti(listing, { cost: 2000, beds: 4 }), 75);
  assert.equal(computeMatchPercentMulti(listing, { cost: Infinity, beds: 4 }), 50);
});
test("missing listing values do not create NaN or a high score", () => {
  assert.equal(computeMatchPercent({ ...listing, price: NaN }, "cost", 2000), 0);
  assert.equal(computeMatchPercent({ ...listing, beds: -1 }, "beds", 2), 0);
});
