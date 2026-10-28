import { createApp } from 'vue'
import App from './App.vue'
import { captureLaunchToken } from './development/client'
import './styles/tokens.css'
import './styles/fonts.css'
import './styles/base.css'

if (import.meta.env.DEV) captureLaunchToken()
createApp(App).mount('#app')
