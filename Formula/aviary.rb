# frozen_string_literal: true

# Installs the Aviary CLI and server from its GitHub release binaries.
class Aviary < Formula
  desc 'Aviary: the AI Agent Nest'
  homepage 'https://aviary.bot'
  license 'MIT'
  version '1.0.8'

  on_macos do
    on_arm do
      url 'https://github.com/lsegal/aviary/releases/download/v1.0.8/aviary_v1.0.8_darwin_arm64.tar.gz'
      sha256 '49ea1ebdbad4a3e11d45ec540c534fe7490705d7b0d74ee878da4b51c0b45a15'
    end

    on_intel do
      url 'https://github.com/lsegal/aviary/releases/download/v1.0.8/aviary_v1.0.8_darwin_amd64.tar.gz'
      sha256 'ed196161a1f7e6947d793f73e597f81979a52b4061d059bb181eb955a1840b47'
    end
  end

  on_linux do
    on_arm do
      url 'https://github.com/lsegal/aviary/releases/download/v1.0.8/aviary_v1.0.8_linux_arm64.tar.gz'
      sha256 '4f91a242c370812fbfd8f7a2ecb90291f170d842197e4f1b626b1d4d6ce44a93'
    end

    on_intel do
      url 'https://github.com/lsegal/aviary/releases/download/v1.0.8/aviary_v1.0.8_linux_amd64.tar.gz'
      sha256 '6e6ee8e80c51a46d1ea236e6e3d3f1acab5af49ec8caee15011738e18e943692'
    end
  end

  def install
    bin.install 'aviary'
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/aviary version")
  end
end
