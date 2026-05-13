from __future__ import annotations

import asyncio
import threading
from concurrent.futures import Future
from typing import Optional

from src.models import Coordinate


class IPhoneController:
    """Persistent asyncio loop + cached DVT/LocationSimulation connection.

    The first call pays the tunneld + DVT handshake cost (~1-2 seconds).
    Subsequent set/clear calls reuse the open connection and return in
    milliseconds. All work runs on a single dedicated worker thread, so the
    Qt main thread never blocks.
    """

    def __init__(self) -> None:
        self._loop: asyncio.AbstractEventLoop = asyncio.new_event_loop()
        self._thread = threading.Thread(target=self._loop.run_forever, daemon=True, name="iphone-loop")
        self._thread.start()
        self._provider = None
        self._loc = None
        self._rsd = None
        self._play_task: Optional[asyncio.Task] = None
        self._conn_lock: Optional[asyncio.Lock] = None

    def _submit(self, coro) -> Future:
        return asyncio.run_coroutine_threadsafe(coro, self._loop)

    async def _ensure_connected(self) -> None:
        if self._conn_lock is None:
            self._conn_lock = asyncio.Lock()
        async with self._conn_lock:
            if self._loc is not None:
                return
            from pymobiledevice3.services.dvt.instruments.dvt_provider import DvtProvider
            from pymobiledevice3.services.dvt.instruments.location_simulation import LocationSimulation
            from pymobiledevice3.tunneld.api import get_tunneld_devices

            try:
                devices = await get_tunneld_devices()
            except Exception as exc:
                raise RuntimeError(
                    f"無法連線到 tunneld({exc})。請確認 tunneld 已用系統管理員權限啟動。"
                ) from exc
            if not devices:
                raise RuntimeError(
                    "tunneld 沒有偵測到任何 iPhone。請確認 iPhone 已連線且開發者模式已開啟。"
                )
            try:
                self._rsd = devices[0]
                self._provider = DvtProvider(self._rsd)
                await self._provider.connect()
                self._loc = LocationSimulation(self._provider)
                await self._loc.connect()
            except Exception:
                self._loc = None
                if self._provider is not None:
                    try:
                        await self._provider.close()
                    except Exception:
                        pass
                self._provider = None
                self._rsd = None
                raise

    async def _set_location(self, lat: float, lon: float) -> None:
        await self._cancel_play_task()
        await self._ensure_connected()
        await self._loc.set(lat, lon)

    async def _play_route(self, points: list[Coordinate], tick_seconds: float) -> None:
        await self._cancel_play_task()
        await self._ensure_connected()
        self._play_task = asyncio.current_task()
        try:
            for index, point in enumerate(points):
                await self._loc.set(point.lat, point.lon)
                if index < len(points) - 1:
                    await asyncio.sleep(tick_seconds)
        except asyncio.CancelledError:
            return
        finally:
            self._play_task = None

    async def _cancel_play_task(self) -> None:
        task = self._play_task
        if task is not None and not task.done():
            task.cancel()
            try:
                await task
            except (asyncio.CancelledError, Exception):
                pass
        self._play_task = None

    async def _stop(self) -> None:
        await self._cancel_play_task()
        if self._loc is not None:
            try:
                await self._loc.clear()
            except Exception:
                pass

    async def _disconnect(self) -> None:
        await self._cancel_play_task()
        if self._provider is not None:
            try:
                await self._provider.close()
            except Exception:
                pass
        self._provider = None
        self._loc = None
        self._rsd = None

    # ----- Thread-safe public API -----

    def warm_up(self) -> Future:
        """Open the DVT connection ahead of time so the first Start is instant."""
        return self._submit(self._ensure_connected())

    def set_location(self, lat: float, lon: float) -> Future:
        return self._submit(self._set_location(lat, lon))

    def play_route(self, points: list[Coordinate], tick_seconds: float = 1.0) -> Future:
        return self._submit(self._play_route(list(points), tick_seconds))

    def stop(self) -> Future:
        return self._submit(self._stop())

    def disconnect(self) -> Future:
        return self._submit(self._disconnect())

    def shutdown(self) -> None:
        try:
            self.disconnect().result(timeout=3)
        except Exception:
            pass
        self._loop.call_soon_threadsafe(self._loop.stop)
        self._thread.join(timeout=2)
