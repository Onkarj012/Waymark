// Date formatting matching the Go admin templates' time.Format layouts
// (admin.go uses "Jan 2, 2006 at 3:04 PM UTC" and "Jan 2, 2006").

const MONTHS = [
  "Jan", "Feb", "Mar", "Apr", "May", "Jun",
  "Jul", "Aug", "Sep", "Oct", "Nov", "Dec",
];

function pad(n: number): string {
  return n < 10 ? `0${n}` : String(n);
}

/** Formats an ISO timestamp as "Jan 2, 2006 at 3:04 PM UTC". */
export function formatAdminDateTime(iso: string): string {
  const d = new Date(iso);
  const month = MONTHS[d.getUTCMonth()];
  const day = d.getUTCDate();
  const year = d.getUTCFullYear();
  let hours = d.getUTCHours();
  const minutes = pad(d.getUTCMinutes());
  const ampm = hours >= 12 ? "PM" : "AM";
  hours = hours % 12;
  if (hours === 0) hours = 12;
  return `${month} ${day}, ${year} at ${hours}:${minutes} ${ampm} UTC`;
}

/** Formats an ISO timestamp as "Jan 2, 2006". */
export function formatAdminDate(iso: string): string {
  const d = new Date(iso);
  const month = MONTHS[d.getUTCMonth()];
  const day = d.getUTCDate();
  const year = d.getUTCFullYear();
  return `${month} ${day}, ${year}`;
}
