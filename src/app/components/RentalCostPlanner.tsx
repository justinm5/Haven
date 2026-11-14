import { useId, useState } from "react";
import { calculateRentalCosts, type RentalCosts } from "../lib/rentalCosts";

const fields: { key: Exclude<keyof RentalCosts, "rent">; label: string }[] = [
  { key: "utilities", label: "Utilities / month" },
  { key: "parking", label: "Parking / month" },
  { key: "insurance", label: "Renter insurance / month" },
  { key: "otherMonthly", label: "Other monthly fees" },
  { key: "deposit", label: "Security deposit" },
  { key: "applicationFees", label: "Application fees (total)" },
  { key: "moving", label: "Moving costs" },
  { key: "otherUpfront", label: "Other upfront costs" },
];
const dollars = new Intl.NumberFormat("en-US", { style: "currency", currency: "USD" });

export function RentalCostPlanner({ rent }: { rent: number }) {
  const id = useId();
  const [amounts, setAmounts] = useState<Record<string, string>>({});
  const costs = { rent } as RentalCosts;
  const invalid = fields.some(({ key }) => {
    const raw = amounts[key] ?? "";
    const value = Number(raw);
    costs[key] = value;
    return raw.trim() !== "" && (!Number.isFinite(value) || value < 0 || value > 1_000_000);
  });
  const rentInvalid = !Number.isFinite(rent) || rent < 0 || rent > 1_000_000;
  const totals = invalid || rentInvalid ? null : calculateRentalCosts(costs);
  const missing = fields.filter(({ key }) => !amounts[key]?.trim()).length;

  return (
    <section aria-labelledby={`${id}-title`} className="bg-card rounded-2xl p-5 border border-border mb-6">
      <h2 id={`${id}-title`} className="text-lg font-semibold">What will this rental actually cost?</h2>
      <p className="text-sm text-muted-foreground mt-2 mb-4">
        Listed rent: {dollars.format(rent)} / month. Add your estimates below. Blank fees count as $0; they are unknown, not confirmed free.
      </p>
      <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
        {fields.map(({ key, label }) => (
          <label key={key} htmlFor={`${id}-${key}`} className="text-sm">
            {label}
            <input id={`${id}-${key}`} type="number" min="0" max="1000000" step="0.01"
              value={amounts[key] ?? ""} placeholder="Unknown"
              onChange={(event) => setAmounts((previous) => ({ ...previous, [key]: event.target.value }))}
              className="mt-1 w-full rounded-lg border border-border bg-background px-3 py-2" />
          </label>
        ))}
      </div>
      <div aria-live="polite" className="mt-5">
        {totals ? <>
          <dl className="grid grid-cols-1 sm:grid-cols-3 gap-4">
            {[["Monthly total", totals.monthly], ["Cash at move-in", totals.moveIn], ["First-year cash outlay", totals.firstYear]].map(([label, value]) => (
              <div key={label}><dt className="text-xs text-muted-foreground">{label}</dt><dd className="text-xl font-semibold text-[#10B981]">{dollars.format(Number(value))}</dd></div>
            ))}
          </dl>
          <p className="text-xs text-muted-foreground mt-3">{missing > 0 ? `${missing} fee estimates still unknown. ` : ""}Move-in includes the first month and one-time costs. First-year outlay includes the deposit, even if later refunded. Confirm fees with the property manager. Estimates stay on this page and reset when you leave.</p>
        </> : <p role="alert" className="text-sm text-red-500">Enter valid amounts between $0 and $1,000,000.</p>}
      </div>
    </section>
  );
}
