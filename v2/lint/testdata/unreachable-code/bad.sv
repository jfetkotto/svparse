module top;
  function int f(input int x);
    return x;
    x = 1;                       // want
  endfunction
  task t(input int n);
    for (int i = 0; i < n; i++) begin
      if (i == 3) break;
      else continue;
      $display("never");         // want
    end
    if (n > 0) return;
    else return;
    $display("also never");      // want
  endtask
  initial begin : blk
    disable blk;
    $display("skipped");         // want
  end
endmodule
