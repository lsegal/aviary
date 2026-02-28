import { defineStore } from 'pinia'
import { ref, computed } from 'vue'

export const useAuthStore = defineStore('auth', () => {
  // Token is read from the aviary_session cookie set by the server.
  const token = ref<string | null>(getCookieToken())

  const isLoggedIn = computed(() => token.value !== null)

  async function login(inputToken: string): Promise<boolean> {
    const res = await fetch('/api/login', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ token: inputToken }),
    })
    if (res.ok) {
      token.value = inputToken
      return true
    }
    return false
  }

  function logout() {
    token.value = null
    document.cookie = 'aviary_session=; Max-Age=0; path=/'
  }

  function getToken(): string | null {
    return token.value ?? getCookieToken()
  }

  return { isLoggedIn, token, login, logout, getToken }
})

function getCookieToken(): string | null {
  const match = document.cookie.match(/aviary_session=([^;]+)/)
  return match ? decodeURIComponent(match[1]) : null
}
