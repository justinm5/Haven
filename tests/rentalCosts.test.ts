import assert from "node:assert/strict";
import { test } from "node:test";
import { calculateRentalCosts, type RentalCosts } from "../src/app/lib/rentalCosts.ts";

const base: RentalCosts = { rent: 2000, utilities: 150, parking: 50, insurance: 20, otherMonthly: 30, deposit: 2000, applicationFees: 50, moving: 400, otherUpfront: 100 };

test("monthly and move-in totals include all known fees without double counting rent", () => {
  assert.deepEqual(calculateRentalCosts(base), { monthly: 2250, moveIn: 4800, firstYear: 29550 });
});
test("zero extras only count recurring rent and no assumed deposit", () => {
  const costs = Object.fromEntries(Object.keys(base).map(key => [key, 0])) as unknown as RentalCosts;
  costs.rent = 1000;
  assert.deepEqual(calculateRentalCosts(costs), { monthly: 1000, moveIn: 1000, firstYear: 12000 });
});
test("decimal fees are added in cents", () => {
  const totals = calculateRentalCosts({ ...base, rent: 1000.10, utilities: 0.20, parking: 0, insurance: 0, otherMonthly: 0 });
  assert.equal(totals.monthly, 1000.30);
});
test("invalid values cannot silently create misleading totals", () => {
  for (const key of Object.keys(base)) {
    for (const value of [-1, NaN, Infinity, 1_000_001]) {
      assert.throws(() => calculateRentalCosts({ ...base, [key]: value }), RangeError);
    }
  }
});
