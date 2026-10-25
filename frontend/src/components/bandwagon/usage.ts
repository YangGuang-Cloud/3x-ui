export function usagePercent(used: number, total: number): number | null {
  return Number.isFinite(used) && Number.isFinite(total) && used >= 0 && total > 0
    ? (used / total) * 100
    : null;
}
export function speedPercent(bytesPerSecond: number, mbps: number): number | null {
  return usagePercent(bytesPerSecond * 8, mbps * 1e6);
}
