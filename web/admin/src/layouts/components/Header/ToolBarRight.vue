<template>
  <div class="tool-bar-ri">
    <div class="header-icon">
      <AssemblySize id="assemblySize" />
      <Language id="language" />
      <SearchMenu id="searchMenu" />
      <ThemeSetting id="themeSetting" />
      <Fullscreen id="fullscreen" />
    </div>
    <span class="username" :title="auth.admin ? `${auth.admin.username}（${roleLabels[auth.admin.role]}）` : ''"
      >{{ auth.admin?.username }}（{{ auth.admin ? roleLabels[auth.admin.role] : "" }}）</span
    >
    <el-button link @click="$router.push('/account')">我的账号</el-button>
    <el-button text @click="auth.logout()">退出登录</el-button>
  </div>
</template>

<script setup lang="ts">
import { useAuthStore } from "@/stores/modules/auth";
import { roleLabels } from "@/auth/permissions";
const auth = useAuthStore();
import AssemblySize from "./components/AssemblySize.vue";
import Fullscreen from "./components/Fullscreen.vue";
import Language from "./components/Language.vue";
import SearchMenu from "./components/SearchMenu.vue";
import ThemeSetting from "./components/ThemeSetting.vue";
</script>

<style scoped lang="scss">
.tool-bar-ri {
  display: flex;
  align-items: center;
  justify-content: center;
  padding-right: 12px;
  flex-shrink: 0;
  gap: 8px;
  .el-button,
  .el-link {
    white-space: nowrap;
    margin-left: 0;
  }
  .header-icon {
    display: flex;
    align-items: center;
    & > * {
      margin-left: 21px;
      color: var(--el-header-text-color);
    }
  }
  .username {
    margin: 0 8px;
    font-size: 13px;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
    color: var(--el-header-text-color);
  }
}
@media (max-width: 1100px) {
  .tool-bar-ri .header-icon {
    display: none;
  }
}
@media (max-width: 850px) {
  .tool-bar-ri .username {
    max-width: 140px;
  }
}
</style>
