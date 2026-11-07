import { onBeforeUnmount, onMounted, type Ref } from 'vue'
import { animate, spring } from 'animejs'

/**
 * Gives a card a springy response whenever its content height changes.
 * Width stays fixed, so text never rewraps while the card is settling.
 *
 * ResizeObserver callbacks run after layout but before paint, so pinning the
 * old height and starting the spring there means the new layout never shows
 * up for a frame at its natural height.
 */
export function useSpringCard(cardRef: Ref<HTMLElement | null>) {
  let observer: ResizeObserver | null = null
  let animation: ReturnType<typeof animate> | null = null
  let reduceMotion: MediaQueryList | null = null
  let lastHeight = 0
  let initialized = false
  let animating = false

  onMounted(() => {
    const node = cardRef.value
    if (!node) {
      return
    }

    reduceMotion = window.matchMedia('(prefers-reduced-motion: reduce)')
    lastHeight = node.getBoundingClientRect().height
    initialized = true

    observer = new ResizeObserver(() => {
      const card = cardRef.value
      if (!card || !initialized || animating) {
        return
      }

      const height = card.getBoundingClientRect().height
      if (Math.abs(height - lastHeight) < 1) {
        lastHeight = height
        return
      }

      const from = lastHeight
      lastHeight = height

      if (reduceMotion?.matches) {
        return
      }

      animation?.cancel()
      animating = true
      card.style.height = `${from}px`
      card.style.overflow = 'hidden'

      animation = animate(card, {
        height: [`${from}px`, `${height}px`],
        ease: spring({ stiffness: 300, damping: 24, mass: 1 }),
        onComplete: () => {
          card.style.height = ''
          card.style.overflow = ''
          animating = false
          animation = null
        },
      })
    })

    observer.observe(node)
  })

  onBeforeUnmount(() => {
    observer?.disconnect()
    animation?.cancel()
  })
}
