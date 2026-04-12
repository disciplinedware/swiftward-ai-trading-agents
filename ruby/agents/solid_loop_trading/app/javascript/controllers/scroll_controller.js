import { Controller } from "@hotwired/stimulus"

export default class extends Controller {
  connect() {
    this.container = this.element.closest('main') || this.element
    this.isAtBottom = true

    // Initial scroll
    setTimeout(() => this.scrollToBottom(), 100)

    // Track scroll position
    this.scrollHandler = this.updateScrollState.bind(this)
    this.container.addEventListener("scroll", this.scrollHandler)

    // Watch for new messages.
    // Watching 'this.element' (the messages-container) with 'subtree: true'
    // is more robust than watching a specific '#messages' div which might be replaced.
    this.observer = new MutationObserver(() => this.handleContentChange())
    this.observer.observe(this.element, { childList: true, subtree: true })
  }

  disconnect() {
    if (this.observer) this.observer.disconnect()
    if (this.container) this.container.removeEventListener("scroll", this.scrollHandler)
  }

  handleContentChange() {
    if (this.isAtBottom) {
      this.scrollToBottom()
    }
  }

  updateScrollState() {
    const threshold = 100 // generous threshold
    const currentPos = this.container.scrollTop + this.container.clientHeight
    const maxPos = this.container.scrollHeight

    this.isAtBottom = (currentPos >= maxPos - threshold)
  }

  scrollToBottom() {
    if (!this.container) return
    this.container.scrollTop = this.container.scrollHeight
  }

  submit(event) {
    if (event.key === "Enter" && !event.shiftKey) {
      event.preventDefault()
      if (event.target.form) {
        event.target.form.requestSubmit()
        event.target.value = ""
        // Always scroll to bottom when the user sends a message
        setTimeout(() => this.scrollToBottom(), 100)
      }
    }
  }
}
