---
name: medir-consumo
description: Medir la CPU de audiofilus y el consumo de batería de la consola Knulli (RG34XX-SP). Úsala para comparar el modo bolsillo con Songo, comprobar una optimización o responder "cuánto gasta".
---

# Medir consumo

Anota cada medición nueva en `docs/consumo.md`, con la situación exacta (pantalla, governor, qué estaba abierto) y la fecha.

## CPU de un proceso

`ps %CPU` es el promedio desde que arrancó el proceso (incluye el arranque): no sirve. Mide la diferencia de ticks en un intervalo:

```sh
P=$(pgrep -x audiofilus); HZ=$(getconf CLK_TCK)
a=$(awk '{print $14+$15}' /proc/$P/stat); sleep 20; b=$(awk '{print $14+$15}' /proc/$P/stat)
echo "$(( (b-a)*100/(20*HZ) ))% de un núcleo"
```

Para repartirlo por hilos, lo mismo con `/proc/$P/task/*/stat` y `comm`. Para separar etapas (decodificar, remuestrear, convertir): `audiofilus bench <archivo>`.

## Batería (en la consola)

Indicador: `/sys/class/power_supply/axp2202-battery/` (`capacity`, `time_to_empty_now`, `status`). La batería es de 3000 mAh, así que 1 % ≈ 30 mAh.

- **Rápido (2–3 min):** lee `time_to_empty_now` cada 10 s. mA ≈ `capacity/100 * 3000 / (time_to_empty_now/3600)`. Tarda en asentarse y salta mucho; promedia.
- **Fiable (30–60 min):** apunta la hora de cada cambio de `capacity`. Minutos por 1 % → %/h. Lanza un registro con `nohup setsid` para que sobreviva a la sesión SSH.
- `status` debe ser `Discharging`. Tras desconectar el cargador, la estimación se queda congelada unos minutos.
- La retroiluminación real se ve en `/sys/class/disp/disp/attr/sys` (`backlight(  0)` = apagada).

## Comparaciones de referencia

Ver la tabla de `docs/consumo.md`: Songo en "Power Saver" ~255 mA, el menú de Knulli con la pantalla apagada ~300–330 mA. Para que la comparación valga, mide en la misma situación (pantalla apagada, mismo governor, música sonando).

## Frames dibujados

Para comprobar a cuántos FPS dibuja una interfaz de verdad: `timeout 5 strace -f -p <pid> -e trace=ioctl` y cuenta `FBIOPAN_DISPLAY` sobre `/dev/fb0`. El `fps:60.2` del panel no es el de la aplicación.
