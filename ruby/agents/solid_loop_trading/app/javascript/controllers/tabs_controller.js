import { Controller } from "@hotwired/stimulus"

export default class extends Controller {
  static targets = [ "tab", "panel" ]

  change(event) {
    event.preventDefault()
    const targetPanelId = event.currentTarget.dataset.targetPanel

    // Update Tabs Styling
    this.tabTargets.forEach(tab => {
      if (tab.dataset.targetPanel === targetPanelId) {
        tab.classList.add('bg-indigo-50', 'text-indigo-700')
        tab.classList.remove('text-gray-600', 'hover:bg-gray-50', 'hover:text-gray-900')
      } else {
        tab.classList.remove('bg-indigo-50', 'text-indigo-700')
        tab.classList.add('text-gray-600', 'hover:bg-gray-50', 'hover:text-gray-900')
      }
    })

    // Show/Hide Panels
    this.panelTargets.forEach(panel => {
      if (panel.id === targetPanelId) {
        panel.classList.remove('hidden')
      } else {
        panel.classList.add('hidden')
      }
    })
  }
}
