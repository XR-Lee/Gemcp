import { createApp } from 'vue'
import PreviewApp from './PreviewApp.vue'
import { useI18n } from './i18n'
import { installPreviewMock } from './preview-mock'
import '@vue-flow/core/dist/style.css'
import '@vue-flow/core/dist/theme-default.css'
import '@vue-flow/controls/dist/style.css'
import '@vue-flow/minimap/dist/style.css'
import './styles.css'

installPreviewMock()
useI18n().setLocale('zh')
createApp(PreviewApp).mount('#app')
