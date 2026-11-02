/* bridge.h - ALSA PCM playback for MonoModule (the only C in the module). */
#ifndef MM_BRIDGE_H
#define MM_BRIDGE_H
#include <stdint.h>

typedef struct mm_pcm mm_pcm_t;

const char *mm_last_error(void);

/* Opens a playback device with S16_LE interleaved. The channel count and rate
 * must match what the device already negotiated. */
mm_pcm_t *mm_pcm_open(const char *device, unsigned int channels, unsigned int rate,
                      unsigned int period_hint, unsigned int buffer_hint);
unsigned int mm_pcm_period(mm_pcm_t *p);
unsigned int mm_pcm_channels(mm_pcm_t *p);
/* Writes frames; recovers from an underrun once. Returns frames or a negative error. */
int mm_pcm_writei(mm_pcm_t *p, const int16_t *buf, unsigned int frames);
void mm_pcm_close(mm_pcm_t *p);

/* SCHED_FIFO for the calling thread. 0 on success. */
int mm_set_realtime(int priority);
#endif
