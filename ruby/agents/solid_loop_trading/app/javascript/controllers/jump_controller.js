import { Controller } from "@hotwired/stimulus"

export default class extends Controller {
  // Intercept the click event on internal anchor links
  to(event) {
    event.preventDefault()
    const href = event.currentTarget.getAttribute("href")
    if (!href || !href.startsWith("#")) return

    const id = href.substring(1)
    const element = document.getElementById(id)

    if (element) {
      const elementPosition = element.getBoundingClientRect().top
      const offsetPosition = elementPosition + (this.element.closest('main')?.scrollTop || window.pageYOffset)

      const scrollContainer = this.element.closest('main') || window
      scrollContainer.scrollTo({
        top: offsetPosition,
        behavior: "smooth"
      })

      // Update the URL fragment without adding a new history entry
      history.replaceState(null, null, `#${id}`)
    }
  }
}
