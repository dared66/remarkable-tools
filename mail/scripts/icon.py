#!/usr/bin/env python3
import sys
from PySide6.QtGui import QGuiApplication, QImage, QPainter, QPen, QColor
from PySide6.QtCore import Qt, QRectF, QPointF

app = QGuiApplication(sys.argv)
image = QImage(256, 256, QImage.Format_ARGB32)
image.fill(Qt.transparent)
p = QPainter(image)
p.setRenderHint(QPainter.Antialiasing)
p.setPen(QPen(QColor("#171717"), 10, Qt.SolidLine, Qt.RoundCap, Qt.RoundJoin))
p.setBrush(Qt.white)
p.drawRoundedRect(QRectF(25, 57, 206, 148), 12, 12)
p.drawPolyline([QPointF(30, 64), QPointF(128, 138), QPointF(226, 64)])
p.drawLine(QPointF(30, 198), QPointF(98, 128))
p.drawLine(QPointF(226, 198), QPointF(158, 128))
p.end()
if not image.save(sys.argv[1]):
    raise SystemExit("Could not save icon")
