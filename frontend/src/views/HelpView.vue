<template>
  <div class="help">
    <h3>How to use Vecura</h3>
    <ol>
      <li>Open <b>Settings</b> and choose how Vecura should create image search data: a cloud provider, or a local model on your computer.</li>
      <li>For a cloud provider, enter your <b>API key</b>, press <b>Check</b>, choose a model, and press <b>Use selected model</b>.</li>
      <li>For a local model, download the runtime if needed, choose a model file, and press <b>Start Server</b>. You can turn on <b>Start automatically</b> so it is ready when Vecura opens.</li>
      <li>Choose an <b>Indexed folder</b>. Vecura reads the images there and remembers them. New images are picked up automatically when the app starts.</li>
      <li>Use the search bar at the top. Type a few words describing what you want and press Enter.</li>
      <li>Results are shown from newest to oldest. Click an image to open it; use the arrows to browse.</li>
      <li>Use the sidebar to open Settings or this Help page. The button at the top can collapse the sidebar; the theme button at the bottom switches appearance.</li>
    </ol>
    <el-alert type="info" :closable="false" title="Privacy">
      Your images and search data stay on your computer. A cloud provider receives only the text needed to create search data. A local model does not send it over the internet.
    </el-alert>
  </div>
</template>

<script setup></script>

<style scoped>
.help { padding: 2px; line-height: 1.55; color: var(--fg-2); animation: help-enter 0.35s var(--ease) both; }
@keyframes help-enter { from { opacity: 0; transform: translateY(6px); } to { opacity: 1; transform: translateY(0); } }
.help h3 {
  margin: 2px 0 14px;
  font-size: 1.05rem;
  font-weight: 600;
  color: var(--fg);
  letter-spacing: -0.01em;
}
.help ol { padding-left: 20px; margin: 0 0 16px; }
.help li { margin-bottom: 10px; font-size: 0.88rem; transition: color 0.18s var(--ease), transform 0.18s var(--ease); }
.help li:hover { color: var(--fg); transform: translateX(2px); }
.help li::marker { color: var(--fg-3); font-weight: 600; }
.help b { color: var(--fg); font-weight: 600; }
.help :deep(.el-alert) { font-size: 0.84rem; transition: transform 0.2s var(--ease), box-shadow 0.2s var(--ease); }
.help :deep(.el-alert):hover { transform: translateY(-1px); box-shadow: 0 8px 20px rgba(0,0,0,0.16); }
.help :deep(.el-alert__title) { font-size: 0.84rem; }
.help code {
  background: var(--field-bg);
  padding: 1px 5px;
  border-radius: 5px;
  font-size: 0.82em;
  color: var(--fg);
}
</style>
