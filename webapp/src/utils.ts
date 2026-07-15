// Mattermost 사이트 URL 헬퍼
// (Mattermost가 페이지에 inject 하는 window.basename 또는 location 사용)
export default function getSiteURL(): string {
  const win = window as unknown as { basename?: string };
  if (win.basename) {
    return win.basename;
  }
  // location.origin 기준
  return window.location.origin || "";
}
