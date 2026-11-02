#ifndef FAKE_ASOUNDLIB_H
#define FAKE_ASOUNDLIB_H
#include <stdlib.h>
#include <stddef.h>
typedef struct _snd_pcm snd_pcm_t;
typedef struct _snd_pcm_hw_params snd_pcm_hw_params_t;
typedef unsigned long snd_pcm_uframes_t;
typedef long snd_pcm_sframes_t;
#define SND_PCM_STREAM_PLAYBACK 0
#define SND_PCM_ACCESS_RW_INTERLEAVED 3
#define SND_PCM_FORMAT_S16_LE 2
#define snd_pcm_hw_params_alloca(p) do { *(p) = (snd_pcm_hw_params_t *)alloca(64); } while (0)
int snd_pcm_open(snd_pcm_t **, const char *, int, int);
const char *snd_strerror(int);
int snd_pcm_hw_params_any(snd_pcm_t *, snd_pcm_hw_params_t *);
int snd_pcm_hw_params_set_access(snd_pcm_t *, snd_pcm_hw_params_t *, int);
int snd_pcm_hw_params_set_format(snd_pcm_t *, snd_pcm_hw_params_t *, int);
int snd_pcm_hw_params_set_channels(snd_pcm_t *, snd_pcm_hw_params_t *, unsigned int);
int snd_pcm_hw_params_set_rate_near(snd_pcm_t *, snd_pcm_hw_params_t *, unsigned int *, int *);
int snd_pcm_hw_params_set_period_size_near(snd_pcm_t *, snd_pcm_hw_params_t *, snd_pcm_uframes_t *, int *);
int snd_pcm_hw_params_set_buffer_size_near(snd_pcm_t *, snd_pcm_hw_params_t *, snd_pcm_uframes_t *);
int snd_pcm_hw_params(snd_pcm_t *, snd_pcm_hw_params_t *);
int snd_pcm_hw_params_get_period_size(const snd_pcm_hw_params_t *, snd_pcm_uframes_t *, int *);
int snd_pcm_prepare(snd_pcm_t *);
snd_pcm_sframes_t snd_pcm_writei(snd_pcm_t *, const void *, snd_pcm_uframes_t);
int snd_pcm_recover(snd_pcm_t *, int, int);
int snd_pcm_drain(snd_pcm_t *);
int snd_pcm_close(snd_pcm_t *);
#endif
