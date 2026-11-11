import AppKit

let destination = URL(fileURLWithPath: CommandLine.arguments[1])
try FileManager.default.createDirectory(at: destination, withIntermediateDirectories: true)
for size in [16, 32, 128, 256, 512] {
    for scale in [1, 2] {
        let pixels = size * scale
        let image = NSImage(size: NSSize(width: pixels, height: pixels))
        image.lockFocus()
        let bounds = NSRect(x: 0, y: 0, width: pixels, height: pixels)
        let inset = CGFloat(pixels) * 0.05
        NSColor(calibratedRed: 0.06, green: 0.07, blue: 0.08, alpha: 1).setFill()
        NSBezierPath(roundedRect: bounds.insetBy(dx: inset, dy: inset),
                     xRadius: CGFloat(pixels) * 0.22,
                     yRadius: CGFloat(pixels) * 0.22).fill()
        let accent = NSColor(calibratedRed: 0.91, green: 0.59, blue: 0.31, alpha: 1)
        let configuration = NSImage.SymbolConfiguration(paletteColors: [accent])
        let note = NSImage(systemSymbolName: "music.note", accessibilityDescription: nil)!
            .withSymbolConfiguration(configuration)!
        let noteRect = bounds.insetBy(dx: CGFloat(pixels) * 0.26, dy: CGFloat(pixels) * 0.24)
        note.draw(in: noteRect)
        image.unlockFocus()
        let bitmap = NSBitmapImageRep(data: image.tiffRepresentation!)!
        let suffix = scale == 2 ? "@2x" : ""
        let filename = "icon_\(size)x\(size)\(suffix).png"
        try bitmap.representation(using: .png, properties: [:])!.write(to: destination.appendingPathComponent(filename))
    }
}
