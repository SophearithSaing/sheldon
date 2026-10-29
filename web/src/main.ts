import { createApp } from 'vue'
import App from './App.vue'
import { captureLaunchToken } from './runtime/client'
import './styles/tokens.css'
import './styles/fonts.css'
import './styles/base.css'

captureLaunchToken()
createApp(App).mount('#app')
