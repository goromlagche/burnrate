// BurnrateBar shows `burnrate --menubar` as macOS menu bar items.
// burnrate install builds it with swiftc and starts it at login.
import AppKit

struct Item: Decodable {
    let title: String
    let color: String?
    let icon: Data
    let tint: String?
    let lines: [String]
}

final class BurnrateBar: NSObject, NSApplicationDelegate {
    let burnrate = CommandLine.arguments.dropFirst().first ?? NSHomeDirectory() + "/.local/bin/burnrate"
    var statusItems: [NSStatusItem] = []

    func applicationDidFinishLaunching(_ notification: Notification) {
        refresh()
        Timer.scheduledTimer(withTimeInterval: 60, repeats: true) { [weak self] _ in self?.refresh() }
    }

    @objc func refresh() {
        let path = burnrate
        DispatchQueue.global().async {
            let process = Process()
            process.executableURL = URL(fileURLWithPath: path)
            process.arguments = ["--menubar"]
            let pipe = Pipe()
            process.standardOutput = pipe
            guard (try? process.run()) != nil else { return }
            let data = pipe.fileHandleForReading.readDataToEndOfFile()
            process.waitUntilExit()
            guard let items = try? JSONDecoder().decode([Item].self, from: data) else { return }
            DispatchQueue.main.async { self.show(items) }
        }
    }

    func show(_ items: [Item]) {
        while statusItems.count < items.count {
            statusItems.append(NSStatusBar.system.statusItem(withLength: NSStatusItem.variableLength))
        }
        for (statusItem, item) in zip(statusItems, items) {
            var image = NSImage(data: item.icon)
            image?.size = NSSize(width: 16, height: 16)
            if let base = image, let tint = item.tint.flatMap(nsColor) {
                image = tinted(base, tint)
            } else {
                image?.isTemplate = true
            }
            statusItem.button?.image = image
            statusItem.button?.imagePosition = .imageLeading

            var attributes: [NSAttributedString.Key: Any] = [.font: NSFont.monospacedDigitSystemFont(ofSize: 0, weight: .regular)]
            if let color = item.color.flatMap(nsColor) { attributes[.foregroundColor] = color }
            statusItem.button?.attributedTitle = NSAttributedString(string: " " + item.title, attributes: attributes)

            let menu = NSMenu()
            menu.autoenablesItems = false
            for line in item.lines {
                if line == "---" {
                    menu.addItem(.separator())
                    continue
                }
                let menuItem = NSMenuItem(title: line, action: nil, keyEquivalent: "")
                menuItem.attributedTitle = NSAttributedString(string: line, attributes: [.font: NSFont.monospacedSystemFont(ofSize: 12, weight: .regular)])
                menu.addItem(menuItem)
            }
            menu.addItem(.separator())
            menu.addItem(withTitle: "Refresh", action: #selector(refresh), keyEquivalent: "r").target = self
            menu.addItem(withTitle: "Quit", action: #selector(NSApplication.terminate(_:)), keyEquivalent: "q")
            statusItem.menu = menu
        }
    }
}

// tinted paints the icon's shape in one color, like a template image but not monochrome.
func tinted(_ image: NSImage, _ color: NSColor) -> NSImage {
    NSImage(size: image.size, flipped: false) { rect in
        image.draw(in: rect)
        color.set()
        rect.fill(using: .sourceAtop)
        return true
    }
}

func nsColor(_ hex: String) -> NSColor? {
    guard hex.hasPrefix("#"), let v = UInt32(hex.dropFirst(), radix: 16) else { return nil }
    return NSColor(red: CGFloat(v >> 16 & 0xff) / 255, green: CGFloat(v >> 8 & 0xff) / 255, blue: CGFloat(v & 0xff) / 255, alpha: 1)
}

let app = NSApplication.shared
let delegate = BurnrateBar()
app.delegate = delegate
app.setActivationPolicy(.accessory)
app.run()
