<script setup lang="ts">
import { computed } from "vue";
const props = defineProps<{ emotion: string; speaking?: boolean }>();
const closed = computed(() =>
  ["happy", "loving", "delicious", "crying", "sleepy", "relaxed"].includes(
    props.emotion,
  ),
);
const brows = computed(() =>
  props.emotion === "angry"
    ? ["M67 83l20 8", "M113 91l20-8"]
    : ["sad", "crying", "confused"].includes(props.emotion)
      ? ["M67 91l20-8", "M113 83l20 8"]
      : ["M68 84q10-4 19 0", "M113 84q10-4 19 0"],
);
const mouth = computed(() =>
  props.emotion === "sleepy"
    ? "M94 128q6 3 12 0"
    : ["sad", "crying"].includes(props.emotion)
      ? "M88 133q12-10 24 0"
      : ["surprised", "confused"].includes(props.emotion)
        ? "M94 127q6-9 12 0q-6 12-12 0"
        : "M87 126q13 14 26 0",
);
</script>
<template>
  <svg
    class="face-preview"
    viewBox="0 0 200 200"
    role="img"
    :aria-label="`小鸡表情预览：${emotion}`"
  >
    <circle cx="100" cy="105" r="76" fill="#ffe393" />
    <path
      d="M39 121q-25-12-18 13q8 17 24 12M161 121q25-12 18 13q-8 17-24 12"
      fill="#ffce69"
    />
    <path d="M85 29q3-17 15-10q12-7 15 10" fill="#ffd56d" />
    <g fill="none" stroke="#634b39" stroke-width="4" stroke-linecap="round">
      <path :d="brows[0]" />
      <path :d="brows[1]" />
    </g>
    <g fill="#493b32" stroke="#493b32" stroke-width="4" stroke-linecap="round">
      <path v-if="emotion === 'sleepy'" d="M69 108h18" fill="none" />
      <path v-else-if="closed" d="M69 105q9 8 18 0" fill="none" />
      <ellipse
        v-else
        cx="78"
        cy="106"
        rx="5"
        :ry="emotion === 'surprised' ? 10 : 8"
      />
      <path v-if="emotion === 'sleepy'" d="M113 108h18" fill="none" />
      <path
        v-else-if="closed || emotion === 'winking'"
        d="M113 105q9 8 18 0"
        fill="none"
      />
      <ellipse
        v-else
        cx="122"
        cy="106"
        rx="5"
        :ry="emotion === 'surprised' ? 10 : 8"
      />
    </g>
    <circle
      v-if="['loving', 'embarrassed', 'happy'].includes(emotion)"
      cx="61"
      cy="120"
      r="7"
      fill="#f4a4a0"
    />
    <circle
      v-if="['loving', 'embarrassed', 'happy'].includes(emotion)"
      cx="139"
      cy="120"
      r="7"
      fill="#f4a4a0"
    />
    <path
      v-if="emotion === 'crying'"
      d="M74 115q-8 15 0 18q8-3 0-18M126 115q-8 15 0 18q8-3 0-18"
      fill="#84c7e5"
    />
    <path
      :d="mouth"
      class="face-mouth"
      :class="{ speaking }"
      fill="none"
      stroke="#d98245"
      stroke-width="5"
      stroke-linecap="round"
    />
    <path
      v-if="emotion === 'silly' || emotion === 'delicious'"
      d="M99 135q5 15 11 0"
      fill="#ef8e89"
      stroke="#d98245"
      stroke-width="2"
    />
    <path
      v-if="emotion === 'loving'"
      d="M148 57q-7-9-12 0q-5-9-12 0q0 8 12 15q12-7 12-15"
      fill="#eb8c8c"
    />
    <path
      v-if="emotion === 'confident'"
      d="M145 69l3-7 3 7 7 3-7 3-3 7-3-7-7-3z"
      fill="#e7a832"
    />
  </svg>
</template>
