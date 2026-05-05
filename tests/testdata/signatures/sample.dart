// Sample Dart file for signatures extraction tests.
import 'package:flutter/material.dart';

abstract class Animal {
  String get name;
  void speak();
}

class Dog extends Animal {
  Dog(this.name);

  @override
  final String name;

  @override
  void speak() {
    print('Woof! I am $name');
  }

  String fetch(String item) {
    return 'Fetched: $item';
  }
}

mixin CanFly {
  void fly() {
    print('Flying...');
  }
}

enum Status {
  idle,
  running,
  stopped,
}

typedef Callback = void Function(String message);

String greet(String name) {
  return 'Hello, $name!';
}

class Repository<T> {
  final List<T> _items = [];

  factory Repository.empty() {
    return Repository<T>();
  }

  void add(T item) {
    _items.add(item);
  }

  List<T> getAll() {
    return List.unmodifiable(_items);
  }
}
