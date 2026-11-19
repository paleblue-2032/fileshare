"use strict";

const form = document.getElementById("login-form");
const password = document.getElementById("password");
const errorBox = document.getElementById("login-error");
const submit = document.getElementById("login-submit");
const toggle = document.getElementById("toggle-password");

toggle.addEventListener("click", () => {
  const showing = password.type === "text";
  password.type = showing ? "password" : "text";
  toggle.textContent = showing ? "Show" : "Hide";
  toggle.setAttribute("aria-label", showing ? "Show password" : "Hide password");
});

form.addEventListener("submit", async (event) => {
  event.preventDefault();
  errorBox.hidden = true;
  if (!password.value) {
    errorBox.textContent = "Enter your password.";
    errorBox.hidden = false;
    password.focus();
    return;
  }
  submit.disabled = true;
  submit.textContent = "Signing in…";
  try {
    const res = await fetch("/api/login", {
      method: "POST",
      credentials: "same-origin",
      headers: { "Content-Type": "application/json", "X-Fileshare": "1" },
      body: JSON.stringify({ password: password.value }),
    });
    if (res.ok) {
      window.location.replace("/");
      return;
    }
    let message = "Could not sign in.";
    if (res.status === 429) {
      message = "Too many attempts. Wait a few minutes and try again.";
    } else {
      const data = await res.json().catch(() => null);
      if (data && data.error) message = data.error;
    }
    errorBox.textContent = message;
    errorBox.hidden = false;
    password.select();
  } catch {
    errorBox.textContent = "Could not reach the server.";
    errorBox.hidden = false;
  } finally {
    submit.disabled = false;
    submit.textContent = "Sign in";
  }
});
