import { Controller } from "@hotwired/stimulus"

export default class extends Controller {
  static values = {
    reloadUrl: String
  }

  connect() {
    this.handleVisibility = this.handleVisibility.bind(this)
    document.addEventListener("visibilitychange", this.handleVisibility)
  }

  disconnect() {
    document.removeEventListener("visibilitychange", this.handleVisibility)
  }

  handleVisibility() {
    if (document.visibilityState === "visible") {
      // The page was hidden and is now visible again.
      // Flush the stream buffer by triggering a turbo refresh of the current state.
      console.log("Tab refocused. Refreshing state to prevent Turbo Stream storm...");

      if (this.reloadUrlValue) {
        Turbo.visit(this.reloadUrlValue, { action: "replace" });
      } else {
        Turbo.visit(window.location.href, { action: "replace" });
      }
    }
  }
}
