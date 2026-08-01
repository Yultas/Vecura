<template>
  <main class="loading-page" aria-label="Loading Vecura">
    <div class="loading-brand" aria-label="Vecura">
      <span
        v-for="(letter, index) in brand"
        :key="`${letter}-${index}`"
        class="loading-letter"
        :style="{ '--letter-delay': `${index * 0.12}s` }"
      >{{ letter }}</span>
    </div>
  </main>
</template>

<script setup>
const brand = 'Vecura'.split('')
</script>

<style scoped>
.loading-page {
  position: fixed;
  inset: 0;
  z-index: 20;
  display: grid;
  place-items: center;
  overflow: hidden;
  --loading-start: #000000;
  --loading-mid: #3533cd;
  background: linear-gradient(90deg, var(--loading-start) 0%, var(--loading-mid) 50%, var(--loading-start) 100%);
  background-size: 200% 100%;
  animation: loading-gradient 8s ease-in-out infinite alternate;
  will-change: background-position;
}

:global(html[data-theme="light"] .loading-page) {
  --loading-start: #fff7ad;
  --loading-mid: #ffa9f9;
}

.loading-brand {
  display: flex;
  align-items: center;
  color: #fff;
  font-family: var(--font);
  font-size: clamp(3.4rem, 11vw, 8rem);
  font-weight: 700;
  letter-spacing: -0.075em;
  line-height: 1;
  text-shadow: 0 12px 36px rgba(0, 0, 0, 0.22);
}

.loading-letter {
  display: inline-block;
  opacity: 0;
  transform: translateX(-34px) scale(0.92);
  animation: loading-letter-in 0.78s var(--letter-delay) cubic-bezier(0.22, 0.8, 0.36, 1) both;
  will-change: opacity, transform;
}

@keyframes loading-gradient {
  0% { background-position: 0% 50%; }
  100% { background-position: 100% 50%; }
}

@keyframes loading-letter-in {
  0% {
    opacity: 0;
    transform: translateX(-34px) scale(0.92);
  }
  70% {
    opacity: 1;
    transform: translateX(2px) scale(1.01);
  }
  100% {
    opacity: 1;
    transform: translateX(0) scale(1);
  }
}

@media (prefers-reduced-motion: reduce) {
  .loading-page {
    animation: none;
  }
  .loading-letter {
    animation: none;
    opacity: 1;
    transform: none;
  }
}
</style>
