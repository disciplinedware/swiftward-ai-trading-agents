import { Controller } from "@hotwired/stimulus"

export default class extends Controller {
  static targets = [ "backtestingFields", "swiftwardFields", "backtestingRadio", "swiftwardRadio" ]

  connect() {
    this.toggle()
  }

  toggle() {
    if (this.backtestingRadioTarget.checked) {
      this.backtestingFieldsTarget.classList.remove("hidden")
      this.swiftwardFieldsTarget.classList.add("hidden")
    } else {
      this.backtestingFieldsTarget.classList.add("hidden")
      this.swiftwardFieldsTarget.classList.remove("hidden")
    }
  }
}