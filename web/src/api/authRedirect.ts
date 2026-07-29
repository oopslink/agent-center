export function reloadAfterSignin(): void {
  window.location.assign('/');
}

export function redirectAfterAuthLoss(target: '/signin' | '/signup'): void {
  window.location.assign(target);
}
