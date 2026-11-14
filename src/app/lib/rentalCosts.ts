export interface RentalCosts {
  rent: number;
  utilities: number;
  parking: number;
  insurance: number;
  otherMonthly: number;
  deposit: number;
  applicationFees: number;
  moving: number;
  otherUpfront: number;
}

/** Use integer cents so ordinary decimal inputs do not accumulate float errors. */
export function calculateRentalCosts(costs: RentalCosts) {
  const cents = (value: number) => {
    if (!Number.isFinite(value) || value < 0 || value > 1_000_000) {
      throw new RangeError("Enter amounts between 0 and 1,000,000.");
    }
    return Math.round(value * 100);
  };
  const monthlyCents = [costs.rent, costs.utilities, costs.parking, costs.insurance, costs.otherMonthly]
    .reduce((sum, value) => sum + cents(value), 0);
  const upfrontCents = [costs.deposit, costs.applicationFees, costs.moving, costs.otherUpfront]
    .reduce((sum, value) => sum + cents(value), 0);
  return {
    monthly: monthlyCents / 100,
    // Assumes first month's recurring charges are paid at move-in.
    moveIn: (monthlyCents + upfrontCents) / 100,
    firstYear: (monthlyCents * 12 + upfrontCents) / 100,
  };
}
