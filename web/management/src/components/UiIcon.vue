<script setup lang="ts">
import { computed } from "vue";
// Lucide SVG paths, ISC/MIT; see ../../THIRD_PARTY_NOTICES.md.
const props = defineProps<{ name: string }>();
const icons: Record<string, { tag: string; attrs: Record<string, string> }[]> = {
  "layout-dashboard": [
    {
      "tag": "rect",
      "attrs": {
        "width": "7",
        "height": "9",
        "x": "3",
        "y": "3",
        "rx": "1"
      }
    },
    {
      "tag": "rect",
      "attrs": {
        "width": "7",
        "height": "5",
        "x": "14",
        "y": "3",
        "rx": "1"
      }
    },
    {
      "tag": "rect",
      "attrs": {
        "width": "7",
        "height": "9",
        "x": "14",
        "y": "12",
        "rx": "1"
      }
    },
    {
      "tag": "rect",
      "attrs": {
        "width": "7",
        "height": "5",
        "x": "3",
        "y": "16",
        "rx": "1"
      }
    }
  ],
  "house": [
    {
      "tag": "path",
      "attrs": {
        "d": "M15 21v-8a1 1 0 0 0-1-1h-4a1 1 0 0 0-1 1v8"
      }
    },
    {
      "tag": "path",
      "attrs": {
        "d": "M3 10a2 2 0 0 1 .709-1.528l7-6a2 2 0 0 1 2.582 0l7 6A2 2 0 0 1 21 10v9a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z"
      }
    }
  ],
  "bot": [
    {
      "tag": "path",
      "attrs": {
        "d": "M12 8V4H8"
      }
    },
    {
      "tag": "rect",
      "attrs": {
        "width": "16",
        "height": "12",
        "x": "4",
        "y": "8",
        "rx": "2"
      }
    },
    {
      "tag": "path",
      "attrs": {
        "d": "M2 14h2"
      }
    },
    {
      "tag": "path",
      "attrs": {
        "d": "M20 14h2"
      }
    },
    {
      "tag": "path",
      "attrs": {
        "d": "M15 13v2"
      }
    },
    {
      "tag": "path",
      "attrs": {
        "d": "M9 13v2"
      }
    }
  ],
  "messages-square": [
    {
      "tag": "path",
      "attrs": {
        "d": "M16 10a2 2 0 0 1-2 2H6.828a2 2 0 0 0-1.414.586l-2.202 2.202A.71.71 0 0 1 2 14.286V4a2 2 0 0 1 2-2h10a2 2 0 0 1 2 2z"
      }
    },
    {
      "tag": "path",
      "attrs": {
        "d": "M20 9a2 2 0 0 1 2 2v10.286a.71.71 0 0 1-1.212.502l-2.202-2.202A2 2 0 0 0 17.172 19H10a2 2 0 0 1-2-2v-1"
      }
    }
  ],
  "users": [
    {
      "tag": "path",
      "attrs": {
        "d": "M16 21v-2a4 4 0 0 0-4-4H6a4 4 0 0 0-4 4v2"
      }
    },
    {
      "tag": "path",
      "attrs": {
        "d": "M16 3.128a4 4 0 0 1 0 7.744"
      }
    },
    {
      "tag": "path",
      "attrs": {
        "d": "M22 21v-2a4 4 0 0 0-3-3.87"
      }
    },
    {
      "tag": "circle",
      "attrs": {
        "cx": "9",
        "cy": "7",
        "r": "4"
      }
    }
  ],
  "contact": [
    {
      "tag": "path",
      "attrs": {
        "d": "M16 2v2"
      }
    },
    {
      "tag": "path",
      "attrs": {
        "d": "M7 21v-2a2 2 0 012-2h6a2 2 0 012 2v2"
      }
    },
    {
      "tag": "path",
      "attrs": {
        "d": "M8 2v2"
      }
    },
    {
      "tag": "circle",
      "attrs": {
        "cx": "12",
        "cy": "10",
        "r": "3"
      }
    },
    {
      "tag": "rect",
      "attrs": {
        "x": "3",
        "y": "3",
        "width": "18",
        "height": "18",
        "rx": "2"
      }
    }
  ],
  "notebook-pen": [
    {
      "tag": "path",
      "attrs": {
        "d": "M13.4 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2v-7.4"
      }
    },
    {
      "tag": "path",
      "attrs": {
        "d": "M2 6h4"
      }
    },
    {
      "tag": "path",
      "attrs": {
        "d": "M2 10h4"
      }
    },
    {
      "tag": "path",
      "attrs": {
        "d": "M2 14h4"
      }
    },
    {
      "tag": "path",
      "attrs": {
        "d": "M2 18h4"
      }
    },
    {
      "tag": "path",
      "attrs": {
        "d": "M21.378 5.626a1 1 0 1 0-3.004-3.004l-5.01 5.012a2 2 0 0 0-.506.854l-.837 2.87a.5.5 0 0 0 .62.62l2.87-.837a2 2 0 0 0 .854-.506z"
      }
    }
  ],
  "clipboard-list": [
    {
      "tag": "rect",
      "attrs": {
        "width": "8",
        "height": "4",
        "x": "8",
        "y": "2",
        "rx": "1",
        "ry": "1"
      }
    },
    {
      "tag": "path",
      "attrs": {
        "d": "M16 4h2a2 2 0 0 1 2 2v14a2 2 0 0 1-2 2H6a2 2 0 0 1-2-2V6a2 2 0 0 1 2-2h2"
      }
    },
    {
      "tag": "path",
      "attrs": {
        "d": "M12 11h4"
      }
    },
    {
      "tag": "path",
      "attrs": {
        "d": "M12 16h4"
      }
    },
    {
      "tag": "path",
      "attrs": {
        "d": "M8 11h.01"
      }
    },
    {
      "tag": "path",
      "attrs": {
        "d": "M8 16h.01"
      }
    }
  ],
  "settings-2": [
    {
      "tag": "path",
      "attrs": {
        "d": "M14 17H5"
      }
    },
    {
      "tag": "path",
      "attrs": {
        "d": "M19 7h-9"
      }
    },
    {
      "tag": "circle",
      "attrs": {
        "cx": "17",
        "cy": "17",
        "r": "3"
      }
    },
    {
      "tag": "circle",
      "attrs": {
        "cx": "7",
        "cy": "7",
        "r": "3"
      }
    }
  ],
  "arrow-up-right": [
    {
      "tag": "path",
      "attrs": {
        "d": "M7 7h10v10"
      }
    },
    {
      "tag": "path",
      "attrs": {
        "d": "M7 17 17 7"
      }
    }
  ]
};
const shapes = computed(() => icons[props.name] ?? icons.bot);
</script>
<template>
  <svg class="ui-icon" width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true" focusable="false">
    <component :is="shape.tag" v-for="(shape, i) in shapes" :key="i" v-bind="shape.attrs" />
  </svg>
</template>
