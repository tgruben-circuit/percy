import type { NotificationEvent } from "../../../types";

export function browserNotificationHandler(event: NotificationEvent): void {
  if (!document.hidden) return;
  if (typeof Notification === "undefined") return;
  if (Notification.permission !== "granted") return;

  switch (event.type) {
    case "agent_done":
      new Notification("Percy", {
        body: "Agent finished",
        tag: "percy-agent-done",
      });
      break;
    case "agent_error":
      new Notification("Percy", {
        body: "Agent error",
        tag: "percy-agent-error",
      });
      break;
  }
}
