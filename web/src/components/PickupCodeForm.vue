<script setup lang="ts">
import { nextTick, onMounted, ref } from 'vue'
import { normalizeCodeInput } from '../format'

const props = withDefaults(
  defineProps<{
    buttonLabel?: string
    placeholder?: string
    compact?: boolean
    autofocus?: boolean
  }>(),
  {
    buttonLabel: '去取件',
    placeholder: '输入 6 位取件码',
    compact: false,
    autofocus: false,
  },
)

const emit = defineEmits<{
  submit: [code: string]
}>()

const code = ref('')
const error = ref('')
const input = ref<{ focus: () => void } | null>(null)

function onInput(value: string) {
  const normalized = normalizeCodeInput(value)
  if (normalized !== value) {
    code.value = normalized
  }
  error.value = ''
}

function submit() {
  if (code.value.length < 6) {
    error.value = '取件码应为 6 位'
    input.value?.focus()
    return
  }
  emit('submit', code.value)
}

onMounted(async () => {
  if (props.autofocus) {
    await nextTick()
    input.value?.focus()
  }
})
</script>

<template>
  <form class="pickup-code-form" @submit.prevent="submit">
    <div class="pickup-row">
      <el-input
        ref="input"
        v-model="code"
        :size="compact ? 'default' : 'large'"
        :placeholder="placeholder"
        maxlength="6"
        autocomplete="one-time-code"
        autocapitalize="characters"
        spellcheck="false"
        aria-label="取件码"
        class="pickup-input"
        @input="onInput"
      />
      <el-button
        type="primary"
        :size="compact ? 'default' : 'large'"
        native-type="submit"
        :disabled="code.length < 6"
      >
        {{ buttonLabel }}
      </el-button>
    </div>
    <p v-if="error" class="pickup-code-error" role="alert">{{ error }}</p>
  </form>
</template>

<style scoped>
.pickup-code-form {
  width: 100%;
}

.pickup-row {
  display: flex;
  gap: 8px;
  width: 100%;
}

.pickup-input {
  flex: 1;
  min-width: 0;
}

.pickup-input :deep(.el-input__inner) {
  font-family: var(--font-mono);
  font-weight: 600;
  letter-spacing: 0.12em;
  text-transform: uppercase;
}

.pickup-row :deep(.el-button) {
  height: 40px;
  flex: 0 0 auto;
  padding: 0 20px;
  border-radius: 999px;
}

.pickup-code-error {
  margin-top: 5px;
  color: var(--red);
  font-size: 11px;
  line-height: 1.4;
}
</style>
