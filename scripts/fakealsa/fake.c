// Test stand-in for libasound: playback frames are written to $FAKE_PCM_OUT,
// paced at the real rate. Only for off-device smoke tests.
#include <alsa/asoundlib.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <time.h>
struct _snd_pcm { FILE *f; unsigned ch, rate; snd_pcm_uframes_t period; };
struct _snd_pcm_hw_params { unsigned ch, rate; snd_pcm_uframes_t period; };
int snd_pcm_open(snd_pcm_t **p, const char *d, int s, int m) {
  const char *o = getenv("FAKE_PCM_OUT"); if (!o) return -2;
  snd_pcm_t *x = calloc(1, sizeof *x); x->f = fopen(o, "wb"); if (!x->f) return -2; *p = x; return 0; }
const char *snd_strerror(int e) { return "fake alsa error"; }
int snd_pcm_hw_params_any(snd_pcm_t *p, snd_pcm_hw_params_t *h) { memset(h, 0, sizeof *h); return 0; }
int snd_pcm_hw_params_set_access(snd_pcm_t *p, snd_pcm_hw_params_t *h, int a) { return 0; }
int snd_pcm_hw_params_set_format(snd_pcm_t *p, snd_pcm_hw_params_t *h, int a) { return 0; }
int snd_pcm_hw_params_set_channels(snd_pcm_t *p, snd_pcm_hw_params_t *h, unsigned c) { h->ch = c; return 0; }
int snd_pcm_hw_params_set_rate_near(snd_pcm_t *p, snd_pcm_hw_params_t *h, unsigned *r, int *d) { h->rate = *r; return 0; }
int snd_pcm_hw_params_set_period_size_near(snd_pcm_t *p, snd_pcm_hw_params_t *h, snd_pcm_uframes_t *s, int *d) { h->period = *s; return 0; }
int snd_pcm_hw_params_set_buffer_size_near(snd_pcm_t *p, snd_pcm_hw_params_t *h, snd_pcm_uframes_t *s) { return 0; }
int snd_pcm_hw_params(snd_pcm_t *p, snd_pcm_hw_params_t *h) { p->ch = h->ch; p->rate = h->rate; p->period = h->period; return 0; }
int snd_pcm_hw_params_get_period_size(const snd_pcm_hw_params_t *h, snd_pcm_uframes_t *s, int *d) { *s = h->period; return 0; }
int snd_pcm_prepare(snd_pcm_t *p) { return 0; }
snd_pcm_sframes_t snd_pcm_writei(snd_pcm_t *p, const void *b, snd_pcm_uframes_t n) {
  fwrite(b, 2 * p->ch, n, p->f); fflush(p->f);
  struct timespec t = {0, (long)(1e9 * n / p->rate)}; nanosleep(&t, 0); return n; }
int snd_pcm_recover(snd_pcm_t *p, int e, int s) { return 0; }
int snd_pcm_drain(snd_pcm_t *p) { return 0; }
int snd_pcm_close(snd_pcm_t *p) { fclose(p->f); free(p); return 0; }
